package identity

import (
	"context"
	"errors"
	"regexp"
)

const (
	DefaultAPIKeyLifetimeSeconds int64 = 90 * 24 * 60 * 60
	MaxAPIKeyLifetimeSeconds     int64 = 365 * 24 * 60 * 60
)

var (
	ErrKeyNotFound = errors.New("machine key not found")
	ErrKeyConflict = errors.New("machine key changed, expired or revoked")
	apiKeyID       = regexp.MustCompile(`^key_[a-zA-Z0-9]{32,64}$`)
)

// APIKeyMetadata is safe to list. Plaintext secrets and stored hashes never appear.
type APIKeyMetadata struct {
	ID        string   `json:"id"`
	Role      string   `json:"role"`
	Scopes    []string `json:"scopes"`
	CreatedAt int64    `json:"createdAt"`
	ExpiresAt int64    `json:"expiresAt"`
	Version   int64    `json:"version"`
	RevokedAt *int64   `json:"revokedAt,omitempty"`
}

type APIKeyResult struct {
	APIKeyMetadata
	APIKey string `json:"apiKey"`
}

type CreateAPIKeyInput struct {
	Role             string
	Scopes           []string
	ExpiresInSeconds *int64
}

type APIKeyPage struct {
	Items     []APIKeyMetadata `json:"items"`
	NextAfter *string          `json:"nextAfter,omitempty"`
}

func ValidAPIKeyID(id string) bool { return apiKeyID.MatchString(id) }

// CreateAPIKey preserves the default issuance entrypoint for internal callers.
func (s *Service) CreateAPIKey(ctx context.Context, token, kind, role string, scopes ...string) (APIKeyResult, error) {
	return s.IssueAPIKey(ctx, token, kind, CreateAPIKeyInput{Role: role, Scopes: scopes})
}

// Issuance, rotation and revocation share the same administrator-session transaction.
// Storage owns SQL; lifetime, permission and transition rules live here.
func (s *Service) IssueAPIKey(ctx context.Context, token, kind string, input CreateAPIKeyInput) (APIKeyResult, error) {
	role := input.Role
	if role == "" {
		role = "RUN_ORCHESTRATOR"
	}
	scopes, err := resolveMachineScopes(role, input.Scopes)
	if err != nil {
		return APIKeyResult{}, err
	}
	var out APIKeyResult
	err = s.withAdministrator(ctx, token, kind, func(tx Transaction, user User) error {
		now := s.Now().Unix()
		expires, err := apiKeyExpiry(now, input.ExpiresInSeconds, DefaultAPIKeyLifetimeSeconds)
		if err != nil {
			return err
		}
		key := APIKeyMetadata{ID: RandomToken("key_"), Role: role, Scopes: scopes,
			CreatedAt: now, ExpiresAt: expires, Version: 1}
		secret := RandomToken("ogk_")
		if err := tx.CreateAPIKey(user, Digest(secret), key); err != nil {
			return err
		}
		if err := tx.AuditResource("api_key_created", user.ID, "project_api_key", key.ID); err != nil {
			return err
		}
		out = APIKeyResult{APIKeyMetadata: key, APIKey: secret}
		return nil
	})
	if err != nil {
		return APIKeyResult{}, err
	}
	return out, nil
}

func (s *Service) ListAPIKeys(ctx context.Context, token, kind, after string, pageSize int) (APIKeyPage, error) {
	if pageSize < 1 || pageSize > 100 || (after != "" && !ValidAPIKeyID(after)) {
		return APIKeyPage{}, ErrInvalid
	}
	page := APIKeyPage{Items: []APIKeyMetadata{}}
	err := s.withAdministrator(ctx, token, kind, func(tx Transaction, user User) error {
		items, err := tx.APIKeys(user, after, pageSize+1)
		if err != nil {
			return err
		}
		if len(items) > pageSize {
			items = items[:pageSize]
			cursor := items[len(items)-1].ID
			page.NextAfter = &cursor
		}
		page.Items = items
		return nil
	})
	if err != nil {
		return APIKeyPage{}, err
	}
	return page, nil
}

// expectedVersion prevents concurrent rotations or retries from replacing a
// credential whose one-time secret was already returned to another caller.
// Omitted lifetime preserves the original expiry; rotation never revives a key.
func (s *Service) RotateAPIKey(ctx context.Context, token, kind, id string, expectedVersion int64, lifetime *int64) (APIKeyResult, error) {
	if !ValidAPIKeyID(id) || expectedVersion < 1 {
		return APIKeyResult{}, ErrInvalid
	}
	var out APIKeyResult
	err := s.withAdministrator(ctx, token, kind, func(tx Transaction, user User) error {
		key, err := tx.APIKey(user, id)
		if err != nil {
			return err
		}
		now := s.Now().Unix()
		if key.RevokedAt != nil || key.ExpiresAt <= now || key.Version != expectedVersion || key.Version == 1<<63-1 {
			return ErrKeyConflict
		}
		if lifetime != nil {
			key.ExpiresAt, err = apiKeyExpiry(now, lifetime, 0)
			if err != nil {
				return err
			}
		}
		key.Version++
		secret := RandomToken("ogk_")
		if err := tx.ReplaceAPIKey(user, key, Digest(secret)); err != nil {
			return err
		}
		if err := tx.AuditResource("api_key_rotated", user.ID, "project_api_key", id); err != nil {
			return err
		}
		out = APIKeyResult{APIKeyMetadata: key, APIKey: secret}
		return nil
	})
	if err != nil {
		return APIKeyResult{}, err
	}
	return out, nil
}

// Repeating revocation succeeds without creating duplicate audit events.
func (s *Service) RevokeAPIKey(ctx context.Context, token, kind, id string) error {
	if !ValidAPIKeyID(id) {
		return ErrInvalid
	}
	return s.withAdministrator(ctx, token, kind, func(tx Transaction, user User) error {
		key, err := tx.APIKey(user, id)
		if err != nil || key.RevokedAt != nil {
			return err
		}
		if err := tx.RevokeAPIKey(user, id, s.Now().Unix()); err != nil {
			return err
		}
		return tx.AuditResource("api_key_revoked", user.ID, "project_api_key", id)
	})
}

func (s *Service) withAdministrator(ctx context.Context, token, kind string, fn func(Transaction, User) error) error {
	if kind != "browser" && kind != "cli" {
		return ErrUnauthorized
	}
	return s.withSession(ctx, token, kind, fn)
}

func apiKeyExpiry(now int64, requested *int64, fallback int64) (int64, error) {
	lifetime := fallback
	if requested != nil {
		lifetime = *requested
	}
	if lifetime < 60 || lifetime > MaxAPIKeyLifetimeSeconds || now > (1<<63-1)-lifetime {
		return 0, ErrInvalid
	}
	return now + lifetime, nil
}
