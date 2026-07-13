package web

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"path"

	"github.com/gin-gonic/gin"
)

//go:embed templates
var templatesFS embed.FS

var basePath = path.Join(".", "templates")

// Known first-party HTML pages (layout.html + page.html).
var pageTemplateKeys = []string{
	"landing",
	"login",
	"dashboard",
	"usereditor",
	"configeditor",
}

// parsePageTemplates loads each page template once from the embed FS.
// Parse failures return an error instead of panicking at request time.
func parsePageTemplates() (map[string]*template.Template, error) {
	out := make(map[string]*template.Template, len(pageTemplateKeys))
	for _, key := range pageTemplateKeys {
		t, err := template.ParseFS(
			templatesFS,
			path.Join(basePath, "layout.html"),
			path.Join(basePath, key+".html"),
		)
		if err != nil {
			return nil, fmt.Errorf("parse template %q: %w", key, err)
		}
		out[key] = t
	}
	return out, nil
}

func (s *Server) writeTemplate(c *gin.Context, key string, data any) {
	c.Writer.Header().Set("Content-Type", "text/html")

	t, ok := s.pageTemplates[key]
	if !ok {
		c.Writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	buf := bytes.Buffer{}
	if err := t.Execute(&buf, data); err != nil {
		c.Writer.WriteHeader(http.StatusInternalServerError)
		return
	}

	io.Copy(c.Writer, &buf)
}
