package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/sebdanielsson/piecemeal/internal/engine"
)

type categoriesOutput struct {
	Body struct {
		Items []engine.Category `json:"items"`
	}
}

type putCategoryInput struct {
	Name string `path:"name" maxLength:"200"`
	Body struct {
		SavePath string `json:"savePath" doc:"Where torrents in this category are saved; empty uses the default save path."`
		// Seeding goals for the category's torrents that follow the
		// default. Absent ones stay as they are, or follow the global
		// settings in a new category.
		RatioLimit               *float64 `json:"ratioLimit,omitempty" doc:"-2 follows the global setting, -1 is unlimited."`
		SeedingTimeLimit         *int64   `json:"seedingTimeLimit,omitempty" doc:"Minutes. -2 follows the global setting, -1 is unlimited."`
		InactiveSeedingTimeLimit *int64   `json:"inactiveSeedingTimeLimit,omitempty" doc:"Minutes. -2 follows the global setting, -1 is unlimited."`
		SeedGoalAction           *string  `json:"seedGoalAction,omitempty" enum:"default,stop,remove,removeWithData" doc:"What happens when a torrent reaches its seeding goals; default follows the global setting."`
		SeedGoalMode             *string  `json:"seedGoalMode,omitempty" enum:"default,any,all" doc:"Whether any goal or all of them must be met; default follows the global setting."`
	}
}

type putCategoryOutput struct{ Body engine.Category }

type nameInput struct {
	Name string `path:"name" maxLength:"200"`
}

type tagsOutput struct {
	Body struct {
		Items []string `json:"items"`
	}
}

func (a *API) registerCategories() {
	const tag = "Categories and tags"
	huma.Register(a.huma, a.operation(http.MethodGet, "/categories", "listCategories", "List categories", tag, accessUser),
		func(context.Context, *struct{}) (*categoriesOutput, error) {
			out := &categoriesOutput{}
			out.Body.Items = a.Engine.Categories()
			return out, nil
		})

	huma.Register(a.huma, a.operation(http.MethodPut, "/categories/{name}", "putCategory", "Create or change a category", tag, accessUser),
		func(ctx context.Context, in *putCategoryInput) (*putCategoryOutput, error) {
			exists := false
			c := engine.NewCategory(in.Name)
			for _, cur := range a.Engine.Categories() {
				if cur.Name == in.Name {
					c, exists = cur, true
				}
			}
			b := in.Body
			c.SavePath = b.SavePath
			if b.RatioLimit != nil {
				c.RatioLimit = *b.RatioLimit
			}
			if b.SeedingTimeLimit != nil {
				c.SeedingTimeLimit = *b.SeedingTimeLimit
			}
			if b.InactiveSeedingTimeLimit != nil {
				c.InactiveSeedingTimeLimit = *b.InactiveSeedingTimeLimit
			}
			if b.SeedGoalAction != nil {
				c.SeedGoalAction = *b.SeedGoalAction
			}
			if b.SeedGoalMode != nil {
				c.SeedGoalMode = *b.SeedGoalMode
			}
			if err := a.Engine.PutCategory(ctx, c, !exists); err != nil {
				return nil, apiError(err)
			}
			for _, got := range a.Engine.Categories() {
				if got.Name == in.Name {
					c = got
				}
			}
			return &putCategoryOutput{Body: c}, nil
		})

	op := a.operation(http.MethodDelete, "/categories/{name}", "deleteCategory", "Delete a category", tag, accessUser)
	op.DefaultStatus = http.StatusNoContent
	huma.Register(a.huma, op, func(ctx context.Context, in *nameInput) (*struct{}, error) {
		return nil, apiError(a.Engine.DeleteCategories(ctx, in.Name))
	})

	huma.Register(a.huma, a.operation(http.MethodGet, "/tags", "listTags", "List tags", tag, accessUser),
		func(context.Context, *struct{}) (*tagsOutput, error) {
			out := &tagsOutput{}
			out.Body.Items = a.Engine.Tags()
			return out, nil
		})

	op = a.operation(http.MethodPut, "/tags/{name}", "createTag", "Create a tag", tag, accessUser)
	op.DefaultStatus = http.StatusNoContent
	huma.Register(a.huma, op, func(ctx context.Context, in *nameInput) (*struct{}, error) {
		return nil, apiError(a.Engine.CreateTags(ctx, in.Name))
	})

	op = a.operation(http.MethodDelete, "/tags/{name}", "deleteTag", "Delete a tag", tag, accessUser)
	op.DefaultStatus = http.StatusNoContent
	huma.Register(a.huma, op, func(ctx context.Context, in *nameInput) (*struct{}, error) {
		return nil, apiError(a.Engine.DeleteTags(ctx, in.Name))
	})
}
