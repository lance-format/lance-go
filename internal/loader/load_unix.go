//go:build linux || darwin

package loader

import "github.com/ebitengine/purego"

const dlopenFlags = purego.RTLD_NOW | purego.RTLD_LOCAL

func openLibrary(path string) (uintptr, error) {
	return purego.Dlopen(path, dlopenFlags)
}

func lookupSymbol(handle uintptr, name string) (uintptr, error) {
	return purego.Dlsym(handle, name)
}
