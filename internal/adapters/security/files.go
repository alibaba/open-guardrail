package security

import (
	"errors"
	"golang.org/x/term"
	"io"
	"os"
	"strings"
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
func ReadPassword(stdin bool) (string, error) {
	if stdin {
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 131))
		if err != nil || len(data) > 130 {
			return "", errors.New("invalid password input")
		}
		return strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r"), nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", errors.New("use --password-stdin for non-interactive input")
	}
	_, _ = os.Stderr.WriteString("Password: ")
	data, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = os.Stderr.WriteString("\n")
	if err != nil {
		return "", errors.New("cannot read password")
	}
	return string(data), nil
}
