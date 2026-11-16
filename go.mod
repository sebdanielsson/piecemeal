module github.com/sebdanielsson/piecemeal

go 1.27.0

// The web UI lives in this module's tree; keep go ./... out of its npm dependencies.
ignore ./web/node_modules
