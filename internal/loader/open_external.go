//go:build lance_external_lib && (linux || darwin)

package loader

import "os"

func open() (*Library, error) {
	if path := os.Getenv(EnvLibPath); path != "" {
		return openFromEnv(path)
	}
	return openFromSystemPath()
}
