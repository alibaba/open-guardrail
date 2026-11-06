package ctlcmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient(t *testing.T) {
	t.Setenv("OPEN_GUARDRAIL_TOKEN", "")
	paths := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}\n"))
	}))
	defer server.Close()
	for _, tc := range []struct {
		command string
		path    string
	}{
		{"status", "/readyz"},
		{"version", "/api/v1alpha1/meta/version"},
		{"capabilities", "/api/v1alpha1/meta/capabilities"},
	} {
		var out bytes.Buffer
		if err := run([]string{"--server", server.URL, tc.command}, &out); err != nil {
			t.Fatal(err)
		}
		if out.String() != "{}\n" || paths[len(paths)-1] != tc.path {
			t.Fatalf("%s: path=%s response=%s", tc.command, paths[len(paths)-1], out.String())
		}
	}
	t.Setenv("OPEN_GUARDRAIL_TOKEN", "test-only-token")
	var out bytes.Buffer
	if err := run([]string{"--server", server.URL, "status"}, &out); err == nil {
		t.Fatal("token sent over HTTP")
	}
	if len(paths) != 3 {
		t.Fatal("unsafe request reached server")
	}
}
