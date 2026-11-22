package ctlcmd

import (
	"encoding/json"
	"errors"
	"os"
)

type credentialFile struct {
	Server    string `json:"server"`
	Token     string `json:"token"`
	RunID     string `json:"runId,omitempty"`
	ExpiresAt int64  `json:"expiresAt,omitempty"`
}

var errCredentialOutputExists = errors.New("credential output already exists")

func saveNewCredential(path string, issue func() (credentialFile, error)) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return errCredentialOutputExists
		}
		return errors.New("cannot create credential output; use a new file path")
	}
	completed := false
	defer func() {
		_ = file.Close()
		if !completed {
			_ = os.Remove(path)
		}
	}()
	credential, err := issue()
	if err != nil {
		return err
	}
	if err = json.NewEncoder(file).Encode(credential); err != nil {
		return errors.New("credential issued but could not be saved; do not automatically retry")
	}
	if err = file.Sync(); err != nil {
		return errors.New("cannot sync credential output; do not automatically retry")
	}
	completed = true
	return nil
}
