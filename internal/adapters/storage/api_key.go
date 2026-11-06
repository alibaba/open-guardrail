package storage

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/alibaba/open-guardrail/internal/application/identity"
)

const apiKeyColumns = "api_key_id,role,scopes_json,created_at,expires_at,version,revoked_at"

func scanAPIKey(row scanner) (identity.APIKeyMetadata, error) {
	var key identity.APIKeyMetadata
	var scopes string
	var revoked sql.NullInt64
	err := row.Scan(&key.ID, &key.Role, &scopes, &key.CreatedAt, &key.ExpiresAt, &key.Version, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return identity.APIKeyMetadata{}, identity.ErrKeyNotFound
	}
	if err != nil {
		return identity.APIKeyMetadata{}, safe(err)
	}
	if json.Unmarshal([]byte(scopes), &key.Scopes) != nil || key.Scopes == nil {
		return identity.APIKeyMetadata{}, identity.ErrCredentialState
	}
	key.Scopes, err = identity.ValidateMachineScopes(key.Role, key.Scopes)
	if err != nil || !identity.ValidAPIKeyID(key.ID) || key.Version < 1 || key.ExpiresAt <= key.CreatedAt {
		return identity.APIKeyMetadata{}, identity.ErrCredentialState
	}
	if revoked.Valid {
		key.RevokedAt = &revoked.Int64
	}
	return key, nil
}

func (t *identityTx) CreateAPIKey(user identity.User, hash string, key identity.APIKeyMetadata) error {
	scopes, err := json.Marshal(key.Scopes)
	if err != nil {
		return identity.ErrInvalid
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO project_api_key
	 (token_hash,workspace_id,project_id,api_key_id,role,scopes_json,created_at,expires_at,version)
	 VALUES($1,(SELECT id FROM workspace WHERE workspace_id=$2),(SELECT id FROM project WHERE project_id=$3),$4,$5,$6,$7,$8,$9)`,
		hash, user.WorkspaceID, user.ProjectID, key.ID, key.Role, string(scopes), key.CreatedAt, key.ExpiresAt, key.Version)
	return safe(err)
}

func (t *identityTx) APIKey(user identity.User, id string) (identity.APIKeyMetadata, error) {
	return scanAPIKey(t.tx.QueryRowContext(t.ctx, "SELECT "+apiKeyColumns+" FROM project_api_key WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND api_key_id=$3",
		user.WorkspaceID, user.ProjectID, id))
}

func (t *identityTx) APIKeys(user identity.User, after string, limit int) ([]identity.APIKeyMetadata, error) {
	rows, err := t.tx.QueryContext(t.ctx, "SELECT "+apiKeyColumns+" FROM project_api_key WHERE workspace_id=(SELECT id FROM workspace WHERE workspace_id=$1) AND project_id=(SELECT id FROM project WHERE project_id=$2) AND api_key_id>$3 ORDER BY api_key_id LIMIT $4",
		user.WorkspaceID, user.ProjectID, after, limit)
	if err != nil {
		return nil, safe(err)
	}
	defer rows.Close()
	keys := []identity.APIKeyMetadata{}
	for rows.Next() {
		key, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, safe(rows.Err())
}

func (t *identityTx) ReplaceAPIKey(user identity.User, key identity.APIKeyMetadata, hash string) error {
	result, err := t.tx.ExecContext(t.ctx, `UPDATE project_api_key SET token_hash=$1,expires_at=$2,version=$3
	 WHERE api_key_id=$4 AND workspace_id=(SELECT id FROM workspace WHERE workspace_id=$5) AND project_id=(SELECT id FROM project WHERE project_id=$6) AND version=$7 AND revoked_at IS NULL`,
		hash, key.ExpiresAt, key.Version, key.ID, user.WorkspaceID, user.ProjectID, key.Version-1)
	return keyWriteResult(result, err)
}

func (t *identityTx) RevokeAPIKey(user identity.User, id string, now int64) error {
	result, err := t.tx.ExecContext(t.ctx, "UPDATE project_api_key SET revoked_at=$1 WHERE api_key_id=$2 AND workspace_id=(SELECT id FROM workspace WHERE workspace_id=$3) AND project_id=(SELECT id FROM project WHERE project_id=$4) AND revoked_at IS NULL",
		now, id, user.WorkspaceID, user.ProjectID)
	return keyWriteResult(result, err)
}

func keyWriteResult(result sql.Result, err error) error {
	if err != nil {
		return safe(err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return safe(err)
	}
	if count != 1 {
		return identity.ErrKeyConflict
	}
	return nil
}
