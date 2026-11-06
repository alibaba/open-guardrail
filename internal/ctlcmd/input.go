package ctlcmd

import (
	"errors"
	"io"
	"os"
)

// readInputFile checks and reads the same open file. The read limit also applies
// if the file grows after Stat, so input size cannot cause unbounded allocation.
func readInputFile(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("input must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !actual.Mode().IsRegular() || !os.SameFile(info, actual) {
		return nil, errors.New("input file changed")
	}
	return readLimitedInput(file, limit)
}

func readLimitedInput(source io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(source, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("input exceeds size limit")
	}
	return data, nil
}
