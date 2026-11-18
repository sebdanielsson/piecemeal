package api

import (
	"context"
	"net/http"
	"reflect"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/sebdanielsson/piecemeal/internal/auth"
)

func reflectTypeOf[T any]() reflect.Type { return reflect.TypeFor[T]() }

// LoginRequest signs in with the local admin's password.
type LoginRequest struct {
	Username string `json:"username" maxLength:"200"`
	Password string `json:"password" maxLength:"1000"`
}

type loginInput struct{ Body LoginRequest }

type principalOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
	Body      *auth.Principal
}

type meOutput struct{ Body *auth.Principal }

// AuthConfig tells the sign-in page which methods are available.
type AuthConfig struct {
	LocalLogin bool   `json:"localLogin"`
	OIDC       bool   `json:"oidc" doc:"Sign in by sending the browser to /api/piecemeal/v1/auth/oidc/login."`
	OIDCName   string `json:"oidcName,omitempty" doc:"What to call the OIDC provider on the sign-in button."`
}

type authConfigOutput struct{ Body AuthConfig }

type logoutOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
}

type apiKeysOutput struct {
	Body struct {
		Items []auth.APIKey `json:"items"`
	}
}

// CreateAPIKeyRequest creates an API key.
type CreateAPIKeyRequest struct {
	Name  string     `json:"name" minLength:"1" maxLength:"100"`
	Scope auth.Scope `json:"scope" enum:"full,qbittorrent,read-only"`
}

// CreatedAPIKey includes the secret, shown only once.
type CreatedAPIKey struct {
	auth.APIKey
	Key string `json:"key" doc:"The secret. It is not stored and can't be shown again."`
}

type createAPIKeyInput struct{ Body CreateAPIKeyRequest }

type createAPIKeyOutput struct {
	Status int
	Body   CreatedAPIKey
}

type idInput struct {
	ID int64 `path:"id"`
}

type sessionsOutput struct {
	Body struct {
		Items []auth.Session `json:"items"`
	}
}

type sessionIDInput struct {
	ID string `path:"id" pattern:"^[0-9a-f]{64}$"`
}

// User is a user account.
type User struct {
	ID          int64      `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"displayName"`
	Email       string     `json:"email"`
	IsAdmin     bool       `json:"isAdmin"`
	OIDC        bool       `json:"oidc" doc:"Signs in through OIDC."`
	CreatedAt   time.Time  `json:"createdAt"`
	LastLoginAt *time.Time `json:"lastLoginAt,omitempty"`
}

type usersOutput struct {
	Body struct {
		Items []User `json:"items"`
	}
}

type auditInput struct {
	Before int64 `query:"before" doc:"Only entries with a lower id."`
	Limit  int   `query:"limit" minimum:"1" maximum:"500" default:"100"`
}

type auditOutput struct {
	Body struct {
		Items []auth.AuditEntry `json:"items"`
	}
}

func (a *API) registerAuth() {
	const tag = "Auth"
	huma.Register(a.huma, a.operation(http.MethodGet, "/auth/config", "getAuthConfig", "Get the sign-in methods", tag, accessPublic),
		func(context.Context, *struct{}) (*authConfigOutput, error) {
			c := AuthConfig{LocalLogin: a.Auth.Config().LocalLogin}
			if a.OIDC != nil {
				c.OIDC, c.OIDCName = true, a.OIDC.Config().Name
			}
			return &authConfigOutput{Body: c}, nil
		})

	op := a.operation(http.MethodPost, "/auth/login", "login", "Sign in with a password", tag, accessPublic)
	op.Errors = []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests}
	op.Middlewares = huma.Middlewares{func(ctx huma.Context, next func(huma.Context)) {
		// Stash the request for the handler, which needs it for the
		// client address and user agent.
		r, _ := humago.Unwrap(ctx)
		next(huma.WithValue(ctx, requestKey{}, r))
	}}
	huma.Register(a.huma, op, func(ctx context.Context, in *loginInput) (*principalOutput, error) {
		r, _ := ctx.Value(requestKey{}).(*http.Request)
		token, p, err := a.Auth.Login(ctx, r, in.Body.Username, in.Body.Password, auth.KindWeb)
		if err != nil {
			return nil, apiError(err)
		}
		return &principalOutput{SetCookie: *a.Auth.Cookie(auth.CookieWeb, token, a.Auth.SessionMaxAge()), Body: p}, nil
	})

	op = a.operation(http.MethodPost, "/auth/logout", "logout", "Sign out", tag, accessPublic)
	op.DefaultStatus = http.StatusNoContent
	huma.Register(a.huma, op, func(ctx context.Context, _ *struct{}) (*logoutOutput, error) {
		if err := a.Auth.Logout(ctx, Principal(ctx)); err != nil {
			return nil, err
		}
		return &logoutOutput{SetCookie: *a.Auth.Cookie(auth.CookieWeb, "", -1)}, nil
	})

	huma.Register(a.huma, a.operation(http.MethodGet, "/auth/me", "getMe", "Get who is signed in", tag, accessUser),
		func(ctx context.Context, _ *struct{}) (*meOutput, error) {
			return &meOutput{Body: Principal(ctx)}, nil
		})
}

type requestKey struct{}

func (a *API) registerAdmin() {
	const tag = "Access"
	huma.Register(a.huma, a.operation(http.MethodGet, "/api-keys", "listAPIKeys", "List API keys", tag, accessUser),
		func(ctx context.Context, _ *struct{}) (*apiKeysOutput, error) {
			keys, err := a.Auth.ListAPIKeys(ctx, Principal(ctx))
			if err != nil {
				return nil, err
			}
			out := &apiKeysOutput{}
			out.Body.Items = keys
			return out, nil
		})

	op := a.operation(http.MethodPost, "/api-keys", "createAPIKey", "Create an API key", tag, accessUser)
	op.DefaultStatus = http.StatusCreated
	huma.Register(a.huma, op, func(ctx context.Context, in *createAPIKeyInput) (*createAPIKeyOutput, error) {
		p := Principal(ctx)
		if p.Method != "session" {
			return nil, huma.Error403Forbidden("API keys can only be created from a signed-in session")
		}
		k, secret, err := a.Auth.CreateAPIKey(ctx, p, in.Body.Name, in.Body.Scope)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		return &createAPIKeyOutput{Status: http.StatusCreated, Body: CreatedAPIKey{APIKey: k, Key: secret}}, nil
	})

	op = a.operation(http.MethodDelete, "/api-keys/{id}", "deleteAPIKey", "Delete an API key", tag, accessUser)
	op.DefaultStatus = http.StatusNoContent
	huma.Register(a.huma, op, func(ctx context.Context, in *idInput) (*struct{}, error) {
		return nil, apiError(a.Auth.DeleteAPIKey(ctx, Principal(ctx), in.ID))
	})

	huma.Register(a.huma, a.operation(http.MethodGet, "/sessions", "listSessions", "List sessions", tag, accessUser),
		func(ctx context.Context, _ *struct{}) (*sessionsOutput, error) {
			sessions, err := a.Auth.ListSessions(ctx, Principal(ctx))
			if err != nil {
				return nil, err
			}
			out := &sessionsOutput{}
			out.Body.Items = sessions
			return out, nil
		})

	op = a.operation(http.MethodDelete, "/sessions/{id}", "deleteSession", "End a session", tag, accessUser)
	op.DefaultStatus = http.StatusNoContent
	huma.Register(a.huma, op, func(ctx context.Context, in *sessionIDInput) (*struct{}, error) {
		return nil, apiError(a.Auth.DeleteSession(ctx, Principal(ctx), in.ID))
	})

	huma.Register(a.huma, a.operation(http.MethodGet, "/users", "listUsers", "List users", tag, accessAdmin),
		func(ctx context.Context, _ *struct{}) (*usersOutput, error) {
			rows, err := a.Auth.Users(ctx)
			if err != nil {
				return nil, err
			}
			out := &usersOutput{}
			out.Body.Items = make([]User, 0, len(rows))
			for _, u := range rows {
				user := User{
					ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Email: u.Email, IsAdmin: u.IsAdmin,
					OIDC: u.OidcSubject.Valid, CreatedAt: time.Unix(u.CreatedAt, 0).UTC(),
				}
				if u.LastLoginAt.Valid {
					t := time.Unix(u.LastLoginAt.Int64, 0).UTC()
					user.LastLoginAt = &t
				}
				out.Body.Items = append(out.Body.Items, user)
			}
			return out, nil
		})

	huma.Register(a.huma, a.operation(http.MethodGet, "/audit", "listAudit", "List audit log entries, newest first", tag, accessAdmin),
		func(ctx context.Context, in *auditInput) (*auditOutput, error) {
			entries, err := a.Auth.ListAudit(ctx, in.Before, in.Limit)
			if err != nil {
				return nil, err
			}
			out := &auditOutput{}
			out.Body.Items = entries
			return out, nil
		})
}
