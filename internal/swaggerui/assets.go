package swaggerui

import "embed"

// Files contains the embedded Swagger UI static distribution.
//
//go:embed dist
var Files embed.FS
