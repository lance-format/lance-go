// Command namespace-consumer opens a native directory namespace without
// importing the root lance-go package. The public-consumer lane builds this
// source in a standalone module so the dependency graph proves that the
// namespace package registers and extracts its own bundle.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/lance-format/lance-go/namespace"
)

func main() {
	root := flag.String("root", "", "existing empty directory for the namespace root")
	flag.Parse()
	if *root == "" {
		log.Fatal("-root is required")
	}

	ctx := context.Background()
	directory, err := namespace.NewDirectory(map[string]string{"root": *root})
	if err != nil {
		log.Fatalf("open namespace: %v", err)
	}

	id, err := directory.ID()
	if err != nil {
		log.Fatalf("namespace id: %v", err)
	}
	if id == "" {
		log.Fatal("namespace id is empty")
	}

	listed, err := directory.ListNamespaces(ctx, &namespace.ListNamespacesRequest{})
	if err != nil {
		log.Fatalf("list namespaces: %v", err)
	}
	if len(listed.Namespaces) != 0 {
		log.Fatalf("expected an empty namespace root, got %d namespaces", len(listed.Namespaces))
	}

	if err := directory.Close(); err != nil {
		log.Fatalf("close namespace: %v", err)
	}
	fmt.Printf("namespace id=%q namespaces=%d\n", id, len(listed.Namespaces))
}
