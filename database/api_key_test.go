package database

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lib/pq"
	"github.com/stretchr/testify/assert"
)

func TestCreateAPIKey_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()

	ds := Datasource{Conn: db}

	name := "Test API Key"
	ownerID := "owner123"
	scopes := []string{"read", "write"}
	expiresAt := time.Now().Add(24 * time.Hour)

	mock.ExpectExec("INSERT INTO ledgerforge.api_keys").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), name, ownerID, pq.StringArray(scopes), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), false).
		WillReturnResult(sqlmock.NewResult(1, 1))

	apiKey, err := ds.CreateAPIKey(context.Background(), name, ownerID, scopes, expiresAt)
	assert.NoError(t, err)
	assert.NotNil(t, apiKey)
	assert.Equal(t, name, apiKey.Name)
	assert.Equal(t, ownerID, apiKey.OwnerID)
	assert.Equal(t, scopes, apiKey.Scopes)
	assert.NotEmpty(t, apiKey.APIKeyID)
	assert.NotEmpty(t, apiKey.Key)
	assert.False(t, apiKey.IsRevoked)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestCreateAPIKey_DatabaseError(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()

	ds := Datasource{Conn: db}

	name := "Test API Key"
	ownerID := "owner123"
	scopes := []string{"read", "write"}
	expiresAt := time.Now().Add(24 * time.Hour)

	mock.ExpectExec("INSERT INTO ledgerforge.api_keys").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), name, ownerID, pq.StringArray(scopes), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), false).
		WillReturnError(sql.ErrConnDone)

	apiKey, err := ds.CreateAPIKey(context.Background(), name, ownerID, scopes, expiresAt)
	assert.Error(t, err)
	assert.Nil(t, apiKey)
	assert.Equal(t, sql.ErrConnDone, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestGetAPIKey_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()

	ds := Datasource{Conn: db}

	key := "test-api-key"
	keyPrefix := getKeyPrefix(key)
	hashedKey, err := hashAPIKey(key)
	assert.NoError(t, err)
	apiKeyID := "api_key_123"
	name := "Test API Key"
	ownerID := "owner123"
	scopes := []string{"read", "write"}
	expiresAt := time.Now().Add(24 * time.Hour)
	createdAt := time.Now().Add(-1 * time.Hour)
	lastUsedAt := time.Now().Add(-30 * time.Minute)

	row := sqlmock.NewRows([]string{"api_key_id", "key", "name", "owner_id", "scopes", "expires_at", "created_at", "last_used_at", "is_revoked", "revoked_at", "key_prefix"}).
		AddRow(apiKeyID, hashedKey, name, ownerID, pq.StringArray(scopes), expiresAt, createdAt, lastUsedAt, false, nil, keyPrefix)

	expectedQuery := regexp.QuoteMeta("SELECT api_key_id, key, name, owner_id, scopes, expires_at, created_at, last_used_at, is_revoked, revoked_at, key_prefix FROM ledgerforge.api_keys WHERE key_prefix = $1")
	mock.ExpectQuery(expectedQuery).
		WithArgs(keyPrefix).
		WillReturnRows(row)

	apiKey, err := ds.GetAPIKey(context.Background(), key)
	assert.NoError(t, err)
	assert.NotNil(t, apiKey)
	assert.Equal(t, apiKeyID, apiKey.APIKeyID)
	assert.Empty(t, apiKey.Key)
	assert.Equal(t, name, apiKey.Name)
	assert.Equal(t, ownerID, apiKey.OwnerID)
	assert.Equal(t, scopes, apiKey.Scopes)
	assert.False(t, apiKey.IsRevoked)
	assert.Nil(t, apiKey.RevokedAt)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}
func TestGetAPIKey_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()

	ds := Datasource{Conn: db}

	key := "non-existent-key"
	keyPrefix := getKeyPrefix(key)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT api_key_id, key, name, owner_id, scopes, expires_at, created_at, last_used_at, is_revoked, revoked_at, key_prefix FROM ledgerforge.api_keys WHERE key_prefix = $1")).
		WithArgs(keyPrefix).
		WillReturnRows(sqlmock.NewRows([]string{"api_key_id", "key", "name", "owner_id", "scopes", "expires_at", "created_at", "last_used_at", "is_revoked", "revoked_at", "key_prefix"}))

	mock.ExpectQuery(regexp.QuoteMeta(legacyAPIKeyQuery)).WillReturnRows(sqlmock.NewRows([]string{"api_key_id", "key", "name", "owner_id", "scopes", "expires_at", "created_at", "last_used_at", "is_revoked", "revoked_at", "key_prefix"}))

	apiKey, err := ds.GetAPIKey(context.Background(), key)
	assert.Error(t, err)
	assert.Nil(t, apiKey)
	assert.Equal(t, ErrAPIKeyNotFound, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestGetAPIKey_DatabaseError(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()

	ds := Datasource{Conn: db}

	key := "test-api-key"
	keyPrefix := getKeyPrefix(key)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT api_key_id, key, name, owner_id, scopes, expires_at, created_at, last_used_at, is_revoked, revoked_at, key_prefix FROM ledgerforge.api_keys WHERE key_prefix = $1")).
		WithArgs(keyPrefix).
		WillReturnError(sql.ErrConnDone)

	apiKey, err := ds.GetAPIKey(context.Background(), key)
	assert.Error(t, err)
	assert.Nil(t, apiKey)
	assert.Equal(t, sql.ErrConnDone, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestRevokeAPIKey(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rows     int64
		dbErr    error
		expected error
	}{
		{"success", 1, nil, nil}, {"not found", 0, nil, ErrAPIKeyNotFound}, {"database error", 0, sql.ErrConnDone, sql.ErrConnDone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			assert.NoError(t, err)
			defer func() { _ = db.Close() }()
			ds := Datasource{Conn: db}
			expectation := mock.ExpectExec("UPDATE ledgerforge.api_keys.*SET is_revoked.*WHERE api_key_id.*AND owner_id.*").WithArgs(sqlmock.AnyArg(), "key-id", "owner")
			if tc.dbErr != nil {
				expectation.WillReturnError(tc.dbErr)
			} else {
				expectation.WillReturnResult(sqlmock.NewResult(0, tc.rows))
			}
			assert.ErrorIs(t, ds.RevokeAPIKey(context.Background(), "key-id", "owner"), tc.expected)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUpdateLastUsed_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()

	ds := Datasource{Conn: db}

	apiKeyID := "api_key_123"

	mock.ExpectExec("UPDATE ledgerforge.api_keys SET last_used_at = \\$1 WHERE api_key_id = \\$2").
		WithArgs(sqlmock.AnyArg(), apiKeyID).
		WillReturnResult(sqlmock.NewResult(1, 1))

	err = ds.UpdateLastUsed(context.Background(), apiKeyID)
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestUpdateLastUsed_DatabaseError(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()

	ds := Datasource{Conn: db}

	apiKeyID := "api_key_123"

	mock.ExpectExec("UPDATE ledgerforge.api_keys SET last_used_at = \\$1 WHERE api_key_id = \\$2").
		WithArgs(sqlmock.AnyArg(), apiKeyID).
		WillReturnError(sql.ErrConnDone)

	err = ds.UpdateLastUsed(context.Background(), apiKeyID)
	assert.Error(t, err)
	assert.Equal(t, sql.ErrConnDone, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestListAPIKeys_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()

	ds := Datasource{Conn: db}

	ownerID := "owner123"
	expiresAt := time.Now().Add(24 * time.Hour)
	createdAt := time.Now().Add(-1 * time.Hour)
	lastUsedAt := time.Now().Add(-30 * time.Minute)

	rows := sqlmock.NewRows([]string{"api_key_id", "key", "name", "owner_id", "scopes", "expires_at", "created_at", "last_used_at", "is_revoked", "revoked_at"}).
		AddRow("api_key_1", "key1", "API Key 1", ownerID, pq.StringArray([]string{"read"}), expiresAt, createdAt, lastUsedAt, false, nil).
		AddRow("api_key_2", "key2", "API Key 2", ownerID, pq.StringArray([]string{"read", "write"}), expiresAt, createdAt, lastUsedAt, true, &createdAt)

	mock.ExpectQuery("SELECT api_key_id, key, name, owner_id, scopes, expires_at, created_at, last_used_at, is_revoked, revoked_at FROM ledgerforge.api_keys WHERE owner_id = \\$1 ORDER BY created_at DESC").
		WithArgs(ownerID).
		WillReturnRows(rows)

	apiKeys, err := ds.ListAPIKeys(context.Background(), ownerID)
	assert.NoError(t, err)
	assert.Len(t, apiKeys, 2)

	// Check first API key
	assert.Equal(t, "api_key_1", apiKeys[0].APIKeyID)
	assert.Empty(t, apiKeys[0].Key)
	assert.Equal(t, "API Key 1", apiKeys[0].Name)
	assert.Equal(t, ownerID, apiKeys[0].OwnerID)
	assert.Equal(t, []string{"read"}, apiKeys[0].Scopes)
	assert.False(t, apiKeys[0].IsRevoked)
	assert.Nil(t, apiKeys[0].RevokedAt)

	// Check second API key
	assert.Equal(t, "api_key_2", apiKeys[1].APIKeyID)
	assert.Empty(t, apiKeys[1].Key)
	assert.Equal(t, "API Key 2", apiKeys[1].Name)
	assert.Equal(t, ownerID, apiKeys[1].OwnerID)
	assert.Equal(t, []string{"read", "write"}, apiKeys[1].Scopes)
	assert.True(t, apiKeys[1].IsRevoked)
	assert.NotNil(t, apiKeys[1].RevokedAt)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestListAPIKeys_EmptyResult(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()

	ds := Datasource{Conn: db}

	ownerID := "owner123"

	rows := sqlmock.NewRows([]string{"api_key_id", "key", "name", "owner_id", "scopes", "expires_at", "created_at", "last_used_at", "is_revoked", "revoked_at"})

	mock.ExpectQuery("SELECT api_key_id, key, name, owner_id, scopes, expires_at, created_at, last_used_at, is_revoked, revoked_at FROM ledgerforge.api_keys WHERE owner_id = \\$1 ORDER BY created_at DESC").
		WithArgs(ownerID).
		WillReturnRows(rows)

	apiKeys, err := ds.ListAPIKeys(context.Background(), ownerID)
	assert.NoError(t, err)
	assert.Len(t, apiKeys, 0)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestListAPIKeys_DatabaseError(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()

	ds := Datasource{Conn: db}

	ownerID := "owner123"

	mock.ExpectQuery("SELECT api_key_id, key, name, owner_id, scopes, expires_at, created_at, last_used_at, is_revoked, revoked_at FROM ledgerforge.api_keys WHERE owner_id = \\$1 ORDER BY created_at DESC").
		WithArgs(ownerID).
		WillReturnError(sql.ErrConnDone)

	apiKeys, err := ds.ListAPIKeys(context.Background(), ownerID)
	assert.Error(t, err)
	assert.Nil(t, apiKeys)
	assert.Equal(t, sql.ErrConnDone, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestListAPIKeys_ScanError(t *testing.T) {
	db, mock, err := sqlmock.New()
	assert.NoError(t, err)
	defer func() { _ = db.Close() }()

	ds := Datasource{Conn: db}

	ownerID := "owner123"

	// Create a row with invalid data type for scopes to trigger scan error
	rows := sqlmock.NewRows([]string{"api_key_id", "key", "name", "owner_id", "scopes", "expires_at", "created_at", "last_used_at", "is_revoked", "revoked_at"}).
		AddRow("api_key_1", "key1", "API Key 1", ownerID, "invalid_scopes_type", time.Now(), time.Now(), time.Now(), false, nil)

	mock.ExpectQuery("SELECT api_key_id, key, name, owner_id, scopes, expires_at, created_at, last_used_at, is_revoked, revoked_at FROM ledgerforge.api_keys WHERE owner_id = \\$1 ORDER BY created_at DESC").
		WithArgs(ownerID).
		WillReturnRows(rows)

	apiKeys, err := ds.ListAPIKeys(context.Background(), ownerID)
	assert.Error(t, err)
	assert.Nil(t, apiKeys)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}
