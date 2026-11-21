package storage

import (
	"context"
	"testing"
	"time"
)

func identityStore(t *testing.T, backend string) *Store {
	t.Helper()
	c := testConfig(t, backend)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}
