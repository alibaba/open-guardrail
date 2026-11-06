package run

import (
	"bytes"
	"errors"
	"testing"
)

func TestCanonicalizeArbitraryObject(t *testing.T) {
	first, firstDigest, err := Canonicalize([]byte(`{"nested":{"ok":true,"values":[1,1.0,null]},"name":"deploy"}`))
	if err != nil {
		t.Fatal(err)
	}
	second, secondDigest, err := Canonicalize([]byte(" { \"name\" : \"deploy\", \"nested\": {\"values\":[1,1.0,null],\"ok\":true} } "))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) || firstDigest != secondDigest {
		t.Fatalf("canonical forms differ: %s %s", first, second)
	}
}

func TestCanonicalizeRejectsAmbiguousOrNonObjectInput(t *testing.T) {
	for _, raw := range []string{`[]`, `null`, `{"a":1,"a":2}`, `{"a":}`, ``} {
		if _, _, err := Canonicalize([]byte(raw)); !errors.Is(err, ErrInvalidChangeSet) {
			t.Fatalf("%q: got %v", raw, err)
		}
	}
}
