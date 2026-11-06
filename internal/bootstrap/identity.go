package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/alibaba/open-guardrail/internal/adapters/security"
	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/identity"
	"github.com/alibaba/open-guardrail/internal/config"
)

// prepareIdentity keeps a persistent host-only token file across restarts.
// Database expiry is authoritative; an expired token requires a server restart.
func prepareIdentity(ctx context.Context, c config.Config, store *storage.Store) (*identity.Service, error) {
	svc := identity.New(store.Identity(), security.Passwords{})
	initialized, err := svc.Status(ctx)
	if err != nil {
		return nil, err
	}
	if initialized {
		return svc, nil
	}
	if err = os.MkdirAll(c.DataDir, 0700); err != nil {
		return nil, errors.New("cannot create private bootstrap directory")
	}
	// OpenRoot prevents symlink escapes from the selected data directory.
	root, err := os.OpenRoot(c.DataDir)
	if err != nil {
		return nil, errors.New("cannot open bootstrap directory")
	}
	defer root.Close()
	const name = "bootstrap.token"
	token := ""
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err == nil {
		token = identity.RandomToken("ogb_")
		_, err = f.WriteString(token + "\n")
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			return nil, errors.New("cannot persist bootstrap token")
		}
	} else if errors.Is(err, os.ErrExist) {
		info, e := root.Lstat(name)
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 128 {
			return nil, errors.New("bootstrap token must be a regular file with permissions 0600")
		}
		data, e := root.ReadFile(name)
		if e != nil || len(data) > 128 {
			return nil, errors.New("cannot read bootstrap token")
		}
		token = strings.TrimSpace(string(data))
	} else {
		return nil, errors.New("cannot create bootstrap token file")
	}
	if !strings.HasPrefix(token, "ogb_") || len(token) < 36 {
		return nil, errors.New("invalid bootstrap token file")
	}
	if err = svc.PrepareBootstrap(ctx, token); err != nil && !errors.Is(err, identity.ErrInitialized) {
		return nil, err
	}
	return svc, nil
}

func bootstrapPath(c config.Config) string { return filepath.Join(c.DataDir, "bootstrap.token") }
