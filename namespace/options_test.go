package namespace_test

import (
	"testing"

	"github.com/lance-format/lance-go/namespace"
)

type nilContextProvider struct{}

func (*nilContextProvider) ProvideContext(string, string) (map[string]string, error) {
	return nil, nil
}

func TestDynamicContextProviderRejectsNil(t *testing.T) {
	_, err := namespace.NewDirectory(nil, namespace.WithDynamicContextProvider(nil))
	if err == nil {
		t.Fatal("WithDynamicContextProvider(nil) succeeded")
	}
	var typedNil *nilContextProvider
	_, err = namespace.NewDirectory(nil, namespace.WithDynamicContextProvider(typedNil))
	if err == nil {
		t.Fatal("WithDynamicContextProvider(typed nil) succeeded")
	}
}
