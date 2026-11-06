package servercmd

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

func TestResetRequiresTerminal(t *testing.T) {
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	err = resetAdmin(context.Background(), []string{"reset"}, input, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("non-interactive input accepted: %v", err)
	}
}

func TestReadResetLineDoesNotReadAhead(t *testing.T) {
	input := strings.NewReader("admin\r\npassword\n")
	line, err := readResetLine(input)
	if err != nil || line != "admin" {
		t.Fatalf("line=%q err=%v", line, err)
	}
	rest, _ := io.ReadAll(input)
	if string(rest) != "password\n" {
		t.Fatal("consumed subsequent input")
	}
}
