package dashboard

import (
	_ "embed"
	"net/http"
	"strings"
)

//go:embed dashboard.html
var rawHTML string

// Handler serves the web dashboard UI.
func Handler(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(HTML(version)))
	}
}

// HTML generates the dashboard single-page web app.
func HTML(version string) string {
	if version == "" {
		version = "dev"
	}
	return strings.ReplaceAll(rawHTML, "{{VERSION}}", version)
}
