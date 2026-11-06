package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPasswords(t *testing.T) {
	p := Passwords{}
	a, err := p.Hash("synthetic-password")
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.Hash("synthetic-password")
	if err != nil {
		t.Fatal(err)
	}
	if a == b || !p.Verify(a, "synthetic-password") || p.Verify(a, "wrong") {
		t.Fatal("invalid password hashing")
	}
	if p.Verify(strings.Replace(a, "m=19456", "m=999999999", 1), "synthetic-password") {
		t.Fatal("unbounded parameters accepted")
	}
}
func TestPrivateFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPrivateFile(path); err != nil {
		t.Fatal(err)
	}
	link := path + ".link"
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPrivateFile(link); err == nil {
		t.Fatal("symlink accepted")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPrivateFile(path); err == nil {
		t.Fatal("public credential file accepted")
	}
}
