// Package web ships the dashboard HTML/CSS/JS as compiled-in assets
// via embed.FS. The dashboard handler renders templates/dashboard.html.tmpl,
// and /static/* is served by a stripped FileServer.
package web

import (
	"embed"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed templates static
var fsys embed.FS

// Templates returns the parsed dashboard template set.
// Panics on parse failure — this is a build-time invariant: the embedded
// templates must always parse cleanly.
func Templates() *template.Template {
	return template.Must(template.ParseFS(fsys, "templates/*.tmpl"))
}

// StaticHandler serves files from the embedded static/ directory under /static/.
// The leading /static prefix is stripped before lookup.
func StaticHandler() http.Handler {
	sub, err := fs.Sub(fsys, "static")
	if err != nil {
		panic(err) // build-time invariant: static/ must exist
	}
	return http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
}
