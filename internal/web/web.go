// Package web serves the built client bundle, embedded into the binary at
// compile time so deployment stays a single artifact. Vite writes its build
// output into this package's dist/ directory (see client/vite.config.ts);
// `make client` keeps the .gitkeep placeholder alive so a checkout without a
// client build still compiles.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distFS embed.FS

// notBuiltBody is served at / when the embedded dist holds no index.html —
// i.e. the binary was built without running the client build first.
const notBuiltBody = "mediumrogue: client bundle not built. Run `make client` and rebuild the server.\n"

// Handler serves the embedded client bundle. The root path serves
// dist/index.html; other paths serve their file or 404. No SPA rewrite is
// needed yet — the client is a single page.
func Handler() http.Handler {
	dist, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Unreachable: "dist" is embedded above. Fail loudly if it ever isn't.
		panic(err)
	}

	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, notBuiltBody, http.StatusServiceUnavailable)
		})
	}

	return http.FileServerFS(dist)
}
