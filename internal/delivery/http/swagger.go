package http

import (
	_ "embed"
	"net/http"

	"github.com/obrel/nexus/docs"
)

//go:embed swagger_ui.html
var swaggerUIHTML []byte

// SwaggerUIHandler serves the Swagger UI page.
func SwaggerUIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(swaggerUIHTML)
	}
}

// OpenAPISpecHandler serves the OpenAPI specification as YAML.
func OpenAPISpecHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-yaml")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write(docs.OpenAPISpec)
	}
}
