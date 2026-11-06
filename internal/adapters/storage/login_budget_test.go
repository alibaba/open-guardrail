package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alibaba/open-guardrail/internal/adapters/security"
	"github.com/alibaba/open-guardrail/internal/application/identity"
)

func TestLoginBudgetPersistsAndRecovers(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			store := identityStore(t, backend.name)
			ctx := context.Background()
			svc := service(store)
			initialize(t, svc)
			now := time.Now()
			svc.Now = func() time.Time { return now }
			input := identity.LoginRequest{Username: "admin", Password: "wrong", Kind: "cli"}
			for i := 0; i < 5; i++ {
				if _, err := svc.Login(ctx, input); !errors.Is(err, identity.ErrUnauthorized) {
					t.Fatal(err)
				}
			}
			// Recreating the service must not reset the shared budget. Changing the
			// attempted username must not bypass the single administrator's quota.
			svc = service(store)
			svc.Now = func() time.Time { return now }
			input.Username = "other"
			if _, err := svc.Login(ctx, input); !errors.Is(err, identity.ErrRateLimited) {
				t.Fatalf("budget did not persist: %v", err)
			}
			before, err := store.Identity().Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			now = now.Add(time.Second)
			if _, err := svc.Login(ctx, input); !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatal(err)
			}
			after, err := store.Identity().Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if after.LoginRetryAt-now.Unix() != 2 {
				t.Fatalf("missing exponential backoff: %+v", after)
			}
			// Rejected requests do not extend the cooldown indefinitely.
			for i := 0; i < 20; i++ {
				if _, err := svc.Login(ctx, input); !errors.Is(err, identity.ErrRateLimited) {
					t.Fatal(err)
				}
			}
			unchanged, err := store.Identity().Status(ctx)
			if err != nil || unchanged.LoginRetryAt != after.LoginRetryAt {
				t.Fatal("rejections extended cooldown")
			}
			if before.LoginAttempts != 5 {
				t.Fatal("attempt reservation was not persisted")
			}
			now = now.Add(31 * time.Second)
			login(t, svc, testPassword)
			reset, err := store.Identity().Status(ctx)
			if err != nil || reset.LoginAttempts != 0 || reset.LoginRetryAt != 0 {
				t.Fatal("success did not reset budget")
			}
			// Host recovery can always clear a temporary login budget.
			for i := 0; i < 5; i++ {
				_, _ = svc.Login(ctx, input)
			}
			if err := svc.ResetLocalAdmin(ctx, "admin", "new-synthetic-password"); err != nil {
				t.Fatal(err)
			}
			login(t, svc, "new-synthetic-password")
		})
	}
}

type pausedPasswords struct {
	inner   identity.Passwords
	entered chan struct{}
	release <-chan struct{}
}

func (p pausedPasswords) Hash(value string) (string, error) { return p.inner.Hash(value) }
func (p pausedPasswords) Verify(hash, value string) bool {
	p.entered <- struct{}{}
	<-p.release
	return p.inner.Verify(hash, value)
}

func TestLoginHashConcurrencyAndResetRace(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			store := identityStore(t, backend.name)
			ctx := context.Background()
			svc := service(store)
			initialize(t, svc)
			session := login(t, svc, testPassword)
			entered, release := make(chan struct{}, 2), make(chan struct{})
			var once sync.Once
			resume := func() { once.Do(func() { close(release) }) }
			t.Cleanup(resume)
			svc.Passwords = pausedPasswords{security.Passwords{}, entered, release}
			results := make(chan error, 2)
			input := identity.LoginRequest{Username: "admin", Password: testPassword, Kind: "cli"}
			for i := 0; i < 2; i++ {
				go func() { _, err := svc.Login(ctx, input); results <- err }()
			}
			for i := 0; i < 2; i++ {
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("password verifier not entered")
				}
			}
			if _, err := svc.Login(ctx, input); !errors.Is(err, identity.ErrRateLimited) {
				t.Fatalf("unbounded concurrent hashing: %v", err)
			}
			limited, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			other := service(store)
			if _, err := other.CreateAPIKey(limited, session.Token, "cli", "RUN_ORCHESTRATOR"); err != nil {
				t.Fatalf("hash held identity lock: %v", err)
			}
			if err := other.ResetLocalAdmin(limited, "admin", "reset-synthetic-password"); err != nil {
				t.Fatal(err)
			}
			resume()
			for i := 0; i < 2; i++ {
				if err := <-results; !errors.Is(err, identity.ErrUnauthorized) {
					t.Fatalf("old password issued a session after reset: %v", err)
				}
			}
			var count int
			if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM admin_session").Scan(&count); err != nil || count != 0 {
				t.Fatal("in-flight login survived reset")
			}
			login(t, other, "reset-synthetic-password")
		})
	}
}

func TestIdentityErrorsKeepCancellationCause(t *testing.T) {
	for _, backend := range registeredDrivers() {
		t.Run(backend.name, func(t *testing.T) {
			store := identityStore(t, backend.name)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err := store.Identity().Status(ctx)
			if !errors.Is(err, context.Canceled) || !errors.Is(err, identity.ErrUnavailable) {
				t.Fatalf("lost canceled read cause: %v", err)
			}
			err = store.Identity().Transact(ctx, func(identity.Transaction) error { return fmt.Errorf("must not run") })
			if !errors.Is(err, context.Canceled) || !errors.Is(err, identity.ErrUnavailable) {
				t.Fatalf("lost canceled transaction cause: %v", err)
			}
		})
	}
}

func TestIdentityStorageErrorDoesNotExposeDriverText(t *testing.T) {
	cause := errors.New("driver error with synthetic-private-password")
	err := safe(cause)
	if !errors.Is(err, cause) || !errors.Is(err, identity.ErrUnavailable) {
		t.Fatal("lost storage cause")
	}
	if strings.Contains(err.Error(), "synthetic-private") {
		t.Fatal("driver text exposed through error string")
	}
}
