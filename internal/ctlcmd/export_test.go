package ctlcmd

import (
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestExportPublicationErrorDistinguishesFilesystemFailure(t *testing.T) {
	for _, test := range []struct {
		name    string
		cause   error
		exit    int
		message string
	}{
		{"existing target", os.ErrExist, exitUsage, "already exists"},
		{"hard links unsupported", syscall.EOPNOTSUPP, exitGeneral, "hard-link support"},
		{"directory permissions", os.ErrPermission, exitGeneral, "directory permissions"},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := exportPublishError(&os.LinkError{Op: "link", Old: "private-temp", New: "private-destination", Err: test.cause})
			if commandExitCode(err) != test.exit || !strings.Contains(err.Error(), test.message) || strings.Contains(err.Error(), "private-") {
				t.Fatalf("incorrect or sensitive export diagnostic: %v", err)
			}
		})
	}
}
