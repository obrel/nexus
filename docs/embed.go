// Package docs provides the embedded OpenAPI specification for the Nexus API.
package docs

import _ "embed"

//go:embed openapi.yaml
var OpenAPISpec []byte
