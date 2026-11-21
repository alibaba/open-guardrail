package httpclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alibaba/open-guardrail/extension/resource"
)

func TestResourceWireContract(t *testing.T) {
	for _, body := range []string{`{"groups":[]}`, `{"groups":[[{"kind":"application","id":"7"}]]}`, `{"groups":null}`, `{"groups":[[]]}`, `{"groups":[[{"Kind":"application","id":"7"}]]}`, `{"groups":[[{"kind":"application","id":"7","policyId":"forged"}]]}`} {
		t.Run(body, func(t *testing.T) {
			plugin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var in map[string]json.RawMessage
				if json.NewDecoder(r.Body).Decode(&in) != nil || string(in["operation"]) != `"bindings.candidates"` || r.URL.Path != "/custom/hooks/resources/" || len(in) != 4 {
					t.Error("incorrect invocation")
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"spiVersion": "v1alpha1", "requestId": in["requestId"], "result": json.RawMessage(body)})
			}))
			defer plugin.Close()
			client, err := New(plugin.URL + "/custom/hooks/resources/")
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ResolveResources(context.Background(), resource.ResolveRequest{})
			valid := body == `{"groups":[]}` || body == `{"groups":[[{"kind":"application","id":"7"}]]}`
			if valid && err != nil || !valid && !errors.Is(err, resource.ErrUnavailable) {
				t.Fatalf("valid=%v err=%v", valid, err)
			}
		})
	}
}

func TestResourceErrorsKeepInvalidAndUnavailableDistinct(t *testing.T) {
	for code, want := range map[string]error{"INVALID_ARGUMENT": resource.ErrInvalid, "PERMISSION_DENIED": resource.ErrUnavailable, "UNAVAILABLE": resource.ErrUnavailable} {
		t.Run(code, func(t *testing.T) {
			plugin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"spiVersion": "v1alpha1", "requestId": input["requestId"], "error": map[string]string{"code": code, "message": "Request failed"}})
			}))
			defer plugin.Close()
			client, err := New(plugin.URL + "/custom/hooks/resources/")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.ResolveResources(context.Background(), resource.ResolveRequest{}); !errors.Is(err, want) {
				t.Fatalf("%s mapped to %v", code, err)
			}
		})
	}
}

func TestChangeSetWirePreservesNullableBusinessFields(t *testing.T) {
	for _, test := range []struct {
		name      string
		changeSet string
		nullError bool
		valid     bool
	}{
		{"nullable-business-fields", `{"commonContext":{"releases":[{"appId":7,"codeBaseLineId":null}]},"optional":null}`, false, true},
		{"null-change-set", `null`, false, false},
		{"duplicate-business-field", `{"appId":7,"appId":8}`, false, false},
		{"null-protocol-field", `{}`, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			plugin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
					t.Error(err)
					return
				}
				if string(input["operation"]) != `"changeset.enrich"` {
					t.Error("incorrect invocation")
				}
				body := map[string]any{
					"spiVersion": "v1alpha1", "requestId": input["requestId"],
					"result": map[string]json.RawMessage{"changeSet": json.RawMessage(test.changeSet)},
				}
				if test.nullError {
					body["error"] = nil
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(body)
			}))
			defer plugin.Close()
			client, err := New(plugin.URL + "/enrich")
			if err != nil {
				t.Fatal(err)
			}
			got, err := client.EnrichChangeSet(context.Background(), resource.ChangeSetRequest{})
			if test.valid {
				if err != nil || string(got) != test.changeSet {
					t.Fatalf("business snapshot changed: %s %v", got, err)
				}
			} else if !errors.Is(err, resource.ErrUnavailable) {
				t.Fatalf("invalid plugin response accepted: %s %v", got, err)
			}
		})
	}
}
