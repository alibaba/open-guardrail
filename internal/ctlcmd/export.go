package ctlcmd

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Publish a complete private file without replacing an existing path. The
// temporary file is in the same directory so publication stays on one filesystem.
func writeExport(path string, data []byte, out io.Writer) error {
	if path == "" {
		_, err := out.Write(data)
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".guardrail-export-*")
	if err != nil {
		return usageError("cannot create export in the requested directory")
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		return internalError("cannot write export")
	}
	if err = file.Sync(); err != nil {
		return internalError("cannot sync export")
	}
	if err = file.Close(); err != nil {
		return internalError("cannot close export")
	}
	if err = os.Link(file.Name(), path); err != nil {
		return exportPublishError(err)
	}
	return nil
}

// Existing destinations are input errors. Other publication failures need a
// filesystem/permissions diagnostic rather than a request to rename the file.
func exportPublishError(err error) error {
	if errors.Is(err, os.ErrExist) {
		return usageError("export path already exists; use a new file path")
	}
	return internalError("cannot publish export; check directory permissions and hard-link support, or omit --output to write to stdout")
}
