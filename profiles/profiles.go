// Package bundled embeds the built-in language profiles into the binary.
// Any binary built from this module carries all profiles and works without
// a profiles/ directory on disk.
package bundled

import "embed"

//go:embed *.yaml
var FS embed.FS
