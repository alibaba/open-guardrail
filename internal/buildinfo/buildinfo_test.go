package buildinfo

import (
	"encoding/json"
	"testing"
)

func TestInfoSerializationNeverExposesCommit(t *testing.T) {
	data, err := json.Marshal(Info{Version: "test-version", Commit: "secret-commit"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"version":"test-version"}` {
		t.Fatalf("unexpected public representation: %s", data)
	}
}
