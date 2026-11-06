package storage

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alibaba/open-guardrail/internal/adapters/security"
	"github.com/alibaba/open-guardrail/internal/application/identity"
)

func TestAPIKeyLifecycle(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			store := identityStore(t, backend.name)
			ctx := context.Background()
			svc := service(store)
			now := time.Now().Unix()
			svc.Now = func() time.Time { return time.Unix(now, 0) }
			initialize(t, svc)
			session := login(t, svc, testPassword)
			key, err := svc.CreateAPIKey(ctx, session.Token, "cli", "RUN_ORCHESTRATOR", identity.ScopeRunsCreateWithRevision)
			if err != nil || !identity.ValidAPIKeyID(key.ID) || key.Version != 1 || key.ExpiresAt-now != identity.DefaultAPIKeyLifetimeSeconds {
				t.Fatalf("invalid issued metadata: %+v %v", key.APIKeyMetadata, err)
			}
			for _, lifetime := range []int64{0, -1, 59, identity.MaxAPIKeyLifetimeSeconds + 1} {
				if _, err := svc.IssueAPIKey(ctx, session.Token, "cli", identity.CreateAPIKeyInput{ExpiresInSeconds: &lifetime}); !errors.Is(err, identity.ErrInvalid) {
					t.Fatalf("invalid lifetime %d accepted: %v", lifetime, err)
				}
			}
			shortLifetime := int64(60)
			short, err := svc.IssueAPIKey(ctx, session.Token, "cli", identity.CreateAPIKeyInput{Role: "POLICY_PUBLISHER", ExpiresInSeconds: &shortLifetime})
			if err != nil {
				t.Fatal(err)
			}
			page, err := svc.ListAPIKeys(ctx, session.Token, "cli", "", 1)
			if err != nil || len(page.Items) != 1 || page.NextAfter == nil {
				t.Fatalf("first page: %+v %v", page, err)
			}
			second, err := svc.ListAPIKeys(ctx, session.Token, "cli", *page.NextAfter, 1)
			if err != nil || len(second.Items) != 1 || second.NextAfter != nil || second.Items[0].ID == page.Items[0].ID {
				t.Fatalf("second page: %+v %v", second, err)
			}
			metadata, _ := json.Marshal(page)
			if strings.Contains(string(metadata), key.APIKey) || strings.Contains(string(metadata), identity.Digest(key.APIKey)) || strings.Contains(string(metadata), "apiKey") {
				t.Fatal("list leaked a secret or stored hash")
			}
			if _, err := svc.ListAPIKeys(ctx, key.APIKey, "key", "", 20); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatal("machine key listed other credentials")
			}
			if err := svc.RevokeAPIKey(ctx, key.APIKey, "key", key.ID); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatal("machine key revoked credentials")
			}
			if _, err := svc.RotateAPIKey(ctx, key.APIKey, "key", key.ID, 1, nil); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatal("machine key rotated credentials")
			}
			// Scope filtering remains part of the SQL boundary, independent of the caller.
			err = store.Identity().Transact(ctx, func(tx identity.Transaction) error {
				_, err := tx.APIKey(identity.User{WorkspaceID: "other", ProjectID: "other"}, key.ID)
				return err
			})
			if !errors.Is(err, identity.ErrKeyNotFound) {
				t.Fatalf("key crossed project scope: %v", err)
			}
			rotated, err := svc.RotateAPIKey(ctx, session.Token, "cli", key.ID, key.Version, nil)
			if err != nil || rotated.ID != key.ID || rotated.Version != 2 || rotated.APIKey == key.APIKey || rotated.ExpiresAt != key.ExpiresAt || !slices.Equal(rotated.Scopes, key.Scopes) {
				t.Fatalf("rotation changed identity, grants or expiry: %+v %v", rotated.APIKeyMetadata, err)
			}
			if _, err := svc.Authenticate(ctx, key.APIKey, "key"); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatal("old secret survived rotation")
			}
			if principal, err := svc.Authenticate(ctx, rotated.APIKey, "key"); err != nil || principal.CredentialID != key.ID {
				t.Fatalf("replacement secret rejected: %v", err)
			}
			if retry, err := svc.RotateAPIKey(ctx, session.Token, "cli", key.ID, 1, nil); !errors.Is(err, identity.ErrKeyConflict) || retry.APIKey != "" {
				t.Fatal("rotation replay replaced the returned credential")
			}
			failing := identity.New(failAuditRepository{Repository: store.Identity()}, security.Passwords{})
			failing.Now = svc.Now
			if result, err := failing.RotateAPIKey(ctx, session.Token, "cli", key.ID, 2, nil); err == nil || result.APIKey != "" {
				t.Fatal("rotation returned a secret despite audit failure")
			}
			if err := failing.RevokeAPIKey(ctx, session.Token, "cli", key.ID); err == nil {
				t.Fatal("revocation succeeded despite audit failure")
			}
			if _, err := svc.Authenticate(ctx, rotated.APIKey, "key"); err != nil {
				t.Fatal("failed audit did not roll back credential changes")
			}
			renewedLifetime := identity.MaxAPIKeyLifetimeSeconds
			renewed, err := svc.RotateAPIKey(ctx, session.Token, "cli", key.ID, rotated.Version, &renewedLifetime)
			if err != nil || renewed.ExpiresAt != now+renewedLifetime || !slices.Equal(renewed.Scopes, rotated.Scopes) {
				t.Fatal("explicit lifetime changed grants or was not applied", err)
			}
			rotated = renewed
			now = short.ExpiresAt
			if _, err := svc.Authenticate(ctx, short.APIKey, "key"); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatal("key accepted at its expiry boundary")
			}
			if _, err := svc.RotateAPIKey(ctx, session.Token, "cli", short.ID, 1, nil); !errors.Is(err, identity.ErrKeyConflict) {
				t.Fatal("rotation revived expired key")
			}
			for range 2 {
				if err := svc.RevokeAPIKey(ctx, session.Token, "cli", key.ID); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := svc.Authenticate(ctx, rotated.APIKey, "key"); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatal("revoked secret accepted")
			}
			if _, err := svc.RotateAPIKey(ctx, session.Token, "cli", key.ID, rotated.Version, nil); !errors.Is(err, identity.ErrKeyConflict) {
				t.Fatal("rotation revived revoked key")
			}
			var events int
			if err := store.db.QueryRow("SELECT COUNT(*) FROM audit_event WHERE event_type='api_key_revoked' AND resource_id=$1", key.ID).Scan(&events); err != nil || events != 1 {
				t.Fatalf("revocation replay duplicated audit: count=%d error=%v", events, err)
			}
			now += int64(identity.SessionLifetime.Seconds())
			if _, err := svc.ListAPIKeys(ctx, session.Token, "cli", "", 20); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatal("expired administrator session listed keys")
			}
			if err := svc.RevokeAPIKey(ctx, session.Token, "cli", short.ID); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatal("expired administrator session revoked a key")
			}
		})
	}
}

func TestConcurrentAPIKeyRotation(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			store := identityStore(t, backend.name)
			svc := service(store)
			initialize(t, svc)
			session := login(t, svc, testPassword)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			key, err := svc.CreateAPIKey(ctx, session.Token, "cli", "POLICY_PUBLISHER")
			if err != nil {
				t.Fatal(err)
			}
			type outcome struct {
				key identity.APIKeyResult
				err error
			}
			results := make(chan outcome, 4)
			start := make(chan struct{})
			var workers sync.WaitGroup
			for range 4 {
				workers.Add(1)
				go func() {
					defer workers.Done()
					<-start
					result, err := svc.RotateAPIKey(ctx, session.Token, "cli", key.ID, 1, nil)
					results <- outcome{result, err}
				}()
			}
			close(start)
			workers.Wait()
			close(results)
			winners := 0
			for result := range results {
				if result.err == nil {
					winners++
					if _, err := svc.Authenticate(ctx, result.key.APIKey, "key"); err != nil {
						t.Fatal("winning secret was replaced", err)
					}
				} else if !errors.Is(result.err, identity.ErrKeyConflict) || result.key.APIKey != "" {
					t.Fatalf("unexpected loser: %v", result.err)
				}
			}
			if winners != 1 {
				t.Fatalf("rotation winners=%d, want one", winners)
			}
		})
	}
}

func TestAPIKeyExpiryAfterConnectionWait(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			store := identityStore(t, backend.name)
			svc := service(store)
			initialize(t, svc)
			session := login(t, svc, testPassword)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			key, err := svc.CreateAPIKey(ctx, session.Token, "cli", "RUN_ORCHESTRATOR")
			if err != nil {
				t.Fatal(err)
			}
			var now atomic.Int64
			now.Store(key.ExpiresAt - 1)
			svc.Now = func() time.Time { return time.Unix(now.Load(), 0) }
			store.db.SetMaxOpenConns(1)
			held, err := store.db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer held.Close()
			waits := store.db.Stats().WaitCount
			result := make(chan error, 1)
			go func() { _, err := svc.Authenticate(ctx, key.APIKey, "key"); result <- err }()
			for store.db.Stats().WaitCount == waits {
				select {
				case <-ctx.Done():
					t.Fatal("authentication did not wait for the held connection")
				case <-time.After(time.Millisecond):
				}
			}
			now.Store(key.ExpiresAt)
			if err := held.Close(); err != nil {
				t.Fatal(err)
			}
			if err := <-result; !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("pool wait extended key lifetime: %v", err)
			}
		})
	}
}
