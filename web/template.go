package web

import (
	"embed"
	"github.com/renorris/openfsd/database"
	"html/template"
	"io"
)

//go:embed templates
var templateFS embed.FS

type DashboardPageData struct {
	UserRecord *database.FSDUserRecord
}

func RenderTemplate(w io.Writer, name string, data interface{}) (err error) {
	var tmpl *template.Template
	if tmpl, err = template.ParseFS(templateFS, "templates/layout.html", "templates/"+name); err != nil {
		return err
	}

	return tmpl.ExecuteTemplate(w, name, data)
}
