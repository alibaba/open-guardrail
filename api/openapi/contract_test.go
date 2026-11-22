package openapi

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// Keep embedded contract references and secret-free response examples checked
// without introducing a second set of client/server DTOs.
func TestContractReferences(t *testing.T) {
	var document map[string]any
	if err := yaml.Unmarshal(Document, &document); err != nil {
		t.Fatal(err)
	}
	var walk func(any)
	walk = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if ref, ok := v["$ref"].(string); ok {
				if !strings.HasPrefix(ref, "#/") {
					t.Fatalf("external reference: %s", ref)
				}
				var current any = document
				for _, key := range strings.Split(ref[2:], "/") {
					object, ok := current.(map[string]any)
					if !ok {
						t.Fatalf("invalid reference: %s", ref)
					}
					current, ok = object[key]
					if !ok {
						t.Fatalf("unresolved reference: %s", ref)
					}
				}
			}
			for _, item := range v {
				walk(item)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	walk(document)

}

func TestEveryDocumentedResponseHasTraceID(t *testing.T) {
	var document map[string]any
	if err := yaml.Unmarshal(Document, &document); err != nil {
		t.Fatal(err)
	}
	paths := document["paths"].(map[string]any)
	for path, pathValue := range paths {
		operations := pathValue.(map[string]any)
		for method, operationValue := range operations {
			operation, ok := operationValue.(map[string]any)
			if !ok {
				continue
			}
			responses := operation["responses"].(map[string]any)
			for status, responseValue := range responses {
				response := resolveObject(t, document, responseValue)
				headers, ok := response["headers"].(map[string]any)
				if !ok {
					t.Fatalf("%s %s response %s has no headers", method, path, status)
				}
				if _, ok = headers["X-Trace-ID"]; !ok {
					t.Fatalf("%s %s response %s has no X-Trace-ID", method, path, status)
				}
			}
		}
	}
}

func resolveObject(t *testing.T, document map[string]any, value any) map[string]any {
	t.Helper()
	object := value.(map[string]any)
	ref, ok := object["$ref"].(string)
	if !ok {
		return object
	}
	if !strings.HasPrefix(ref, "#/") {
		t.Fatalf("external reference: %s", ref)
	}
	var current any = document
	for _, key := range strings.Split(ref[2:], "/") {
		current = current.(map[string]any)[key]
	}
	return current.(map[string]any)
}
