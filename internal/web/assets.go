package web

import "embed"

// Files contains the Vite production build. The placeholder index keeps Go
// builds working before the first frontend build has been run.
//
//go:embed dist
var Files embed.FS
