// Package spa serves the React single-page app from an embedded filesystem.
// At Docker build time the dist/ directory is replaced with the real Vite
// bundle; in local dev it contains a placeholder and the SPA is served by
// Vite on a separate port.
package spa

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler returns an http.Handler that serves the built SPA. Requests for
// real files (e.g. /assets/foo.js) get those files; any other GET is served
// the SPA's index.html so client-side routing works on hard refresh.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		// Try the requested file. If it doesn't exist, fall back to index.html.
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			fileServer.ServeHTTP(w, r)
			return
		}
		if _, err := fs.Stat(sub, p); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, r2)
	})
}
