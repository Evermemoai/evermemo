// Package webui serves the embedded web dashboard. The dist directory is
// populated by scripts/build-web.sh (a static export of the Next.js app in
// web/); without it, a placeholder page explains how to build.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var dist embed.FS

// Handler serves the embedded dashboard with an index.html fallback for
// client-side routes.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return http.NotFoundHandler()
	}
	files := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(sub, p); err != nil {
			// Client-side route: serve the app shell.
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}
