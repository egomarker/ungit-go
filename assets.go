package ungitgoassets

import "embed"

// FS contains the production browser assets and built-in Ungit-Go components.
//
// The development-only Node/npm toolchain builds these frontend files;
// the Go runtime only serves the generated artifacts.
//
//go:embed public components package.json
var FS embed.FS
