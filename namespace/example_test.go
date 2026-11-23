//go:build !lance_external_lib

package namespace_test

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/lance-format/lance-go/namespace"
)

// This example manages namespaces through a local Directory namespace.
// Directory rejects a nil ID, so pass an explicit empty slice to address the
// root namespace.
func ExampleNewDirectory() {
	ctx := context.Background()
	root, err := os.MkdirTemp("", "lance-go-catalog-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(root)

	ns, err := namespace.NewDirectory(map[string]string{"root": root})
	if err != nil {
		log.Fatal(err)
	}
	defer ns.Close()

	rootID := []string{}
	before, err := ns.ListNamespaces(ctx, &namespace.ListNamespacesRequest{ID: &rootID})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("before:", before.Namespaces)

	eventsID := []string{"events"}
	mode := "create"
	if _, err := ns.CreateNamespace(ctx, &namespace.CreateNamespaceRequest{
		ID:   &eventsID,
		Mode: &mode,
	}); err != nil {
		log.Fatal(err)
	}
	if err := ns.NamespaceExists(ctx, &namespace.NamespaceExistsRequest{ID: &eventsID}); err != nil {
		log.Fatal(err)
	}
	fmt.Println("events exists")

	after, err := ns.ListNamespaces(ctx, &namespace.ListNamespacesRequest{ID: &rootID})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("after:", after.Namespaces)

	if _, err := ns.DropNamespace(ctx, &namespace.DropNamespaceRequest{ID: &eventsID}); err != nil {
		log.Fatal(err)
	}
	// Output:
	// before: []
	// events exists
	// after: [events]
}
