package openapi

import _ "embed"

// Spec is the OpenAPI document served at /swagger.json.
//
//go:embed openapi.json
var Spec []byte
