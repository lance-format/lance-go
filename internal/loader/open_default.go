//go:build !lance_external_lib && (linux || darwin)

package loader

import "os"

// open is called with mu held. Bundle registration happens during package
// initialization before any Load call, so reading bundle under mu is safe.
func open() (*Library, error) {
	if path := os.Getenv(EnvLibPath); path != "" {
		return openFromEnv(path)
	}
	if b := bundle; b != nil {
		return b.open()
	}
	return openFromSystemPath()
}
