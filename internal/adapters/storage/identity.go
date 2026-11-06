package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/alibaba/open-guardrail/internal/application/identity"
)

// Identity uses the SQL subset shared by SQLite and PostgreSQL. A write to the
// singleton row serializes identity changes: SQLite obtains its writer lock,
// PostgreSQL obtains a row lock. No process-local lock enforces correctness.
type Identity struct{ db *sql.DB }

func (s *Store) Identity() *Identity { return &Identity{db: s.db} }

type identityTx struct {
	ctx context.Context
	tx  *sql.Tx
}

// Preserve classification without putting driver text (possibly credentials)
// into routine logs. Callers can inspect the cause with errors.Is/errors.As.
type identityStorageError struct{ kind, cause error }

func (e *identityStorageError) Error() string   { return "identity storage: " + e.kind.Error() }
func (e *identityStorageError) Unwrap() []error { return []error{e.kind, e.cause} }
func safe(err error) error {
	if err == nil {
		return nil
	}
	kind := identity.ErrUnavailable
	if errors.Is(err, sql.ErrNoRows) {
		kind = identity.ErrUnauthorized
	}
	return &identityStorageError{kind: kind, cause: err}
}
func (r *Identity) Transact(ctx context.Context, fn func(identity.Transaction) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return safe(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE identity_state SET id=id WHERE id=1"); err != nil {
		return safe(err)
	}
	if err = fn(&identityTx{ctx: ctx, tx: tx}); err != nil {
		return err
	}
	return safe(tx.Commit())
}

type scanner interface{ Scan(...any) error }

func scanState(row scanner) (identity.State, error) {
	var s identity.State
	var initialized int
	err := row.Scan(&initialized, &s.BootstrapHash, &s.BootstrapExpires, &s.LoginAttempts, &s.LoginRetryAt, &s.LoginLastAttempt)
	s.Initialized = initialized == 1
	return s, safe(err)
}

const stateSQL = "SELECT initialized,bootstrap_hash,bootstrap_expires,login_attempts,login_retry_at,login_last_attempt FROM identity_state WHERE id=1"

func (r *Identity) Status(ctx context.Context) (identity.State, error) {
	return scanState(r.db.QueryRowContext(ctx, stateSQL))
}
func (t *identityTx) State() (identity.State, error) {
	return scanState(t.tx.QueryRowContext(t.ctx, stateSQL))
}
func (t *identityTx) SaveState(s identity.State) error {
	initialized := 0
	if s.Initialized {
		initialized = 1
	}
	_, err := t.tx.ExecContext(t.ctx, "UPDATE identity_state SET initialized=$1,bootstrap_hash=$2,bootstrap_expires=$3,login_attempts=$4,login_retry_at=$5,login_last_attempt=$6 WHERE id=1", initialized, s.BootstrapHash, s.BootstrapExpires, s.LoginAttempts, s.LoginRetryAt, s.LoginLastAttempt)
	return safe(err)
}
func scanUser(row scanner) (identity.User, error) {
	var u identity.User
	err := row.Scan(&u.ID, &u.Username, &u.WorkspaceID, &u.ProjectID, &u.PasswordHash)
	return u, safe(err)
}

const userSQL = "SELECT u.user_id,u.username,w.workspace_id,p.project_id,u.password_hash FROM local_user u JOIN workspace w ON w.id=u.workspace_id JOIN project p ON p.id=u.project_id WHERE u.user_id='admin'"

func (t *identityTx) User() (identity.User, error) {
	return scanUser(t.tx.QueryRowContext(t.ctx, userSQL))
}
func (t *identityTx) SaveUser(u identity.User) error {
	_, err := t.tx.ExecContext(t.ctx, "UPDATE local_user SET password_hash=$1,username=$2 WHERE user_id=$3", u.PasswordHash, u.Username, u.ID)
	return safe(err)
}
func (t *identityTx) Initialize(u identity.User) error {
	statements := []struct {
		q    string
		args []any
	}{
		{"INSERT INTO workspace(workspace_id,name) VALUES ($1,$2)", []any{u.WorkspaceID, "Default"}},
		{"INSERT INTO project(project_id,workspace_id,name) VALUES ($1,(SELECT id FROM workspace WHERE workspace_id=$2),$3)", []any{u.ProjectID, u.WorkspaceID, "Default"}},
		{"INSERT INTO local_user(user_id,username,workspace_id,project_id,password_hash) VALUES ($1,$2,(SELECT id FROM workspace WHERE workspace_id=$3),(SELECT id FROM project WHERE project_id=$4),$5)", []any{u.ID, u.Username, u.WorkspaceID, u.ProjectID, u.PasswordHash}},
	}
	for _, st := range statements {
		if _, err := t.tx.ExecContext(t.ctx, st.q, st.args...); err != nil {
			return safe(err)
		}
	}
	return nil
}
func (t *identityTx) Session(hash string) (identity.Session, error) {
	var s identity.Session
	s.Hash = hash
	err := t.tx.QueryRowContext(t.ctx, "SELECT (SELECT user_id FROM local_user WHERE id=admin_session.user_id),kind,expires_at FROM admin_session WHERE token_hash=$1", hash).Scan(&s.UserID, &s.Kind, &s.Expires)
	return s, safe(err)
}
func (t *identityTx) SaveSession(s identity.Session) error {
	if _, err := t.tx.ExecContext(t.ctx, "DELETE FROM admin_session WHERE expires_at <= $1", time.Now().Unix()); err != nil {
		return safe(err)
	}
	_, err := t.tx.ExecContext(t.ctx, "INSERT INTO admin_session(token_hash,user_id,kind,expires_at) VALUES ($1,(SELECT id FROM local_user WHERE user_id=$2),$3,$4)", s.Hash, s.UserID, s.Kind, s.Expires)
	return safe(err)
}
func (t *identityTx) DeleteSession(hash string) error {
	_, err := t.tx.ExecContext(t.ctx, "DELETE FROM admin_session WHERE token_hash=$1", hash)
	return safe(err)
}
func (t *identityTx) DeleteUserSessions(userID string) error {
	_, err := t.tx.ExecContext(t.ctx, "DELETE FROM admin_session WHERE user_id=(SELECT id FROM local_user WHERE user_id=$1)", userID)
	return safe(err)
}
func (t *identityTx) Audit(event, actor string) error {
	// Do not record credentials, submitted usernames, SQL errors or arbitrary client input.
	_, err := t.tx.ExecContext(t.ctx, "INSERT INTO audit_event(event_type,actor_id,workspace_id,project_id,occurred_at) VALUES ($1,$2,(SELECT id FROM workspace WHERE workspace_id=$3),(SELECT id FROM project WHERE project_id=$4),$5)", event, actor, "default", "default", time.Now().Unix())
	return safe(err)
}
func (t *identityTx) AuditResource(event, actor, resourceType, resourceID string) error {
	// Resource IDs may identify stored credential hashes but must never contain plaintext credentials.
	_, err := t.tx.ExecContext(t.ctx, "INSERT INTO audit_event(event_type,actor_id,workspace_id,project_id,occurred_at,resource_type,resource_id) VALUES ($1,$2,(SELECT id FROM workspace WHERE workspace_id=$3),(SELECT id FROM project WHERE project_id=$4),$5,$6,$7)", event, actor, "default", "default", time.Now().Unix(), resourceType, resourceID)
	return safe(err)
}
func (r *Identity) Authenticate(ctx context.Context, hash, kind string, now func() int64) (identity.Principal, error) {
	if now == nil {
		return identity.Principal{}, identity.ErrInvalid
	}
	var p identity.Principal
	if kind == "key" {
		var scopesJSON string
		var expires int64
		var revoked sql.NullInt64
		err := r.db.QueryRowContext(ctx, "SELECT w.workspace_id,p.project_id,k.role,k.scopes_json,k.api_key_id,k.expires_at,k.revoked_at FROM project_api_key k JOIN workspace w ON w.id=k.workspace_id JOIN project p ON p.id=k.project_id WHERE k.token_hash=$1", hash).Scan(&p.WorkspaceID, &p.ProjectID, &p.Role, &scopesJSON, &p.CredentialID, &expires, &revoked)
		if err != nil {
			return identity.Principal{}, safe(err)
		}
		// Database/pool waits must not extend the credential lifetime.
		if expires <= now() || revoked.Valid {
			return identity.Principal{}, identity.ErrUnauthorized
		}
		if !identity.ValidAPIKeyID(p.CredentialID) {
			return identity.Principal{}, identity.ErrCredentialState
		}
		if err = json.Unmarshal([]byte(scopesJSON), &p.Scopes); err != nil || p.Scopes == nil {
			return identity.Principal{}, identity.ErrCredentialState
		}
		p.Scopes, err = identity.ValidateMachineScopes(p.Role, p.Scopes)
		if err != nil {
			return identity.Principal{}, identity.ErrCredentialState
		}
		p.Kind = "api_key"
		return p, nil
	}
	var userID, actualKind string
	var expires int64
	err := r.db.QueryRowContext(ctx, "SELECT (SELECT user_id FROM local_user WHERE id=admin_session.user_id),kind,expires_at FROM admin_session WHERE token_hash=$1", hash).Scan(&userID, &actualKind, &expires)
	if err != nil {
		return p, safe(err)
	}
	if actualKind != kind {
		return p, identity.ErrUnauthorized
	}
	u, err := scanUser(r.db.QueryRowContext(ctx, userSQL))
	if err != nil {
		return p, err
	}
	if u.ID != userID || expires <= now() {
		return p, identity.ErrUnauthorized
	}
	return identity.Principal{Kind: "admin", User: &u, WorkspaceID: u.WorkspaceID, ProjectID: u.ProjectID, Role: "ADMIN"}, nil
}
