package main

import (
	"bytes"
	"embed"
	"github.com/gin-gonic/gin"
	"html/template"
	"io"
	"net/http"
	"path"
)

//go:embed templates
var templatesFS embed.FS

var basePath = path.Join(".", "templates")

func loadTemplate(key string) (t *template.Template) {
	t, err := template.ParseFS(templatesFS, path.Join(basePath, "layout.html"), path.Join(basePath, key+".html"))
	if err != nil {
		panic(err)
	}
	return
}

func writeTemplate(c *gin.Context, key string, data any) {
	c.Writer.Header().Set("Content-Type", "text/html")

	buf := bytes.Buffer{}
	if err := loadTemplate(key).Execute(&buf, nil); err != nil {
		c.Writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	io.Copy(c.Writer, &buf)
}
