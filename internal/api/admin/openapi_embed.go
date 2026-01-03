package admin

import (
	_ "embed"
)

// OpenAPISpec contains the embedded OpenAPI specification.
//
//go:embed openapi.yaml
var OpenAPISpec []byte
