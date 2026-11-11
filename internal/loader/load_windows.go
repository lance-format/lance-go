//go:build windows

package loader

import "golang.org/x/sys/windows"

// openLibrary loads a DLL through the Windows loader. The returned handle is
// never passed to FreeLibrary; the loader keeps lance-c mapped for the life of
// the process, matching the Unix implementation's no-unload contract.
func openLibrary(path string) (uintptr, error) {
	handle, err := windows.LoadLibrary(path)
	if err != nil {
		return 0, err
	}
	return uintptr(handle), nil
}

func lookupSymbol(handle uintptr, name string) (uintptr, error) {
	return windows.GetProcAddress(windows.Handle(handle), name)
}
