package ui

import (
	"embed"
	"html/template"
	"io/fs"
)

//go:embed templates/*.html
var files embed.FS

func Templates() (*template.Template, error) {
	tplFS, err := fs.Sub(files, "templates")
	if err != nil {
		return nil, err
	}
	return template.ParseFS(tplFS, "*.html")
}
