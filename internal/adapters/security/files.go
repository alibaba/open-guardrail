package security

import (
	"errors"
	"io"
	"os"
)

// ReadPrivateFile refuses symlinks, shared files and oversized credential inputs.
func ReadPrivateFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return nil, errors.New("credential file must be a regular file with permissions 0600")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot open credential file")
	}
	defer f.Close()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return nil, errors.New("credential file changed")
	}
	data, err := io.ReadAll(io.LimitReader(f, 8193))
	if err != nil || len(data) > 8192 {
		return nil, errors.New("cannot read credential file")
	}
	return data, nil
}
