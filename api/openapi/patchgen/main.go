// Command patchgen removes three generated net/http registrations that cannot
// be represented by ServeMux wildcard syntax. The model router handles them.
package main

import (
	"bytes"
	"fmt"
	"os"
)

func main() {
	const path = "generated.gen.go"
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	for _, route := range []string{
		"m.HandleFunc(http.MethodPost+\" \"+options.BaseURL+\"/api/v1alpha1/scenes/{key}:initialize\", wrapper.InitializeScene)",
		"m.HandleFunc(http.MethodPost+\" \"+options.BaseURL+\"/api/v1alpha1/policies/{policyId}:archive\", wrapper.ArchivePolicy)",
		"m.HandleFunc(http.MethodPost+\" \"+options.BaseURL+\"/api/v1alpha1/policies/{policyId}:unarchive\", wrapper.UnarchivePolicy)",
	} {
		line := []byte("\t" + route + "\n")
		if bytes.Count(data, line) != 1 {
			panic(fmt.Sprintf("generated route not found once: %s", route))
		}
		data = bytes.Replace(data, line, nil, 1)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		panic(err)
	}
}
