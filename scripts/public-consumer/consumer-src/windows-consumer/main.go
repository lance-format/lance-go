//go:build windows

// Command windows-consumer loads the public namespace API and reports the
// native module that the process actually mapped. It is a focused evidence
// helper for the Windows public-consumer lane, not a second product API.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unsafe"

	"github.com/lance-format/lance-go/namespace"
	"golang.org/x/sys/windows"
)

func main() {
	root := flag.String("root", "", "existing empty directory for the namespace root")
	flag.Parse()
	if *root == "" {
		log.Fatal("-root is required")
	}

	directory, err := namespace.NewDirectory(map[string]string{"root": *root})
	if err != nil {
		log.Fatalf("open namespace: %v", err)
	}
	id, err := directory.ID()
	if err != nil {
		_ = directory.Close()
		log.Fatalf("namespace id: %v", err)
	}
	if id == "" {
		_ = directory.Close()
		log.Fatal("namespace id is empty")
	}
	if err := directory.Close(); err != nil {
		log.Fatalf("close namespace: %v", err)
	}

	modules, err := loadedModules()
	if err != nil {
		log.Fatalf("enumerate loaded modules: %v", err)
	}
	lanceModules := make([]string, 0, 1)
	for _, module := range modules {
		if strings.EqualFold(filepath.Base(module), "lance_c.dll") {
			lanceModules = append(lanceModules, module)
		}
	}
	sort.Strings(lanceModules)
	if len(lanceModules) != 1 {
		log.Fatalf("expected one loaded lance_c.dll, found %d", len(lanceModules))
	}
	info, err := os.Stat(lanceModules[0])
	if err != nil {
		log.Fatalf("stat loaded lance_c.dll: %v", err)
	}
	fmt.Printf("loaded_dll=%s\n", lanceModules[0])
	fmt.Printf("loaded_dll_file_size=%d\n", info.Size())
	fmt.Printf("loaded_module_count=%d\n", len(modules))
	for _, module := range modules {
		fmt.Printf("loaded_module=%s\t%s\n", strings.ToLower(filepath.Base(module)), module)
	}
}

func loadedModules() ([]string, error) {
	process, err := windows.GetCurrentProcess()
	if err != nil {
		return nil, err
	}
	size := uint32(1024)
	var modules []windows.Handle
	for {
		candidates := make([]windows.Handle, size)
		var needed uint32
		err := windows.EnumProcessModulesEx(
			process,
			&candidates[0],
			uint32(len(candidates))*uint32(unsafe.Sizeof(candidates[0])),
			&needed,
			windows.LIST_MODULES_ALL,
		)
		if err != nil {
			return nil, err
		}
		count := needed / uint32(unsafe.Sizeof(candidates[0]))
		if count <= uint32(len(candidates)) {
			modules = candidates[:count]
			break
		}
		size = count + 64
	}

	paths := make([]string, 0, len(modules))
	for _, module := range modules {
		buffer := make([]uint16, 32768)
		if err := windows.GetModuleFileNameEx(
			process,
			module,
			&buffer[0],
			uint32(len(buffer)),
		); err != nil {
			return nil, err
		}
		paths = append(paths, windows.UTF16ToString(buffer))
	}
	sort.Strings(paths)
	return paths, nil
}
