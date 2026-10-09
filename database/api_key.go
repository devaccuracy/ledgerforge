package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/devaccuracy/ledgerforge/internal/policy"
	"github.com/devaccuracy/ledgerforge/model"
	"github.com/lib/pq"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrAPIKeyNotFound               = errors.New("api key not found")
	ErrInvalidAPIKey                = errors.New("invalid api key")
	ErrLegacyAPIKeyVerificationBusy = errors.New("legacy API key verification is busy")
)

// Legacy scans are deliberately fail-fast: unauthenticated traffic cannot
// queue unlimited bcrypt work or occupy every database connection.
const maxLegacyAPIKeyVerifications = 2

var legacyAPIKeyVerificationSlots = make(chan struct{}, maxLegacyAPIKeyVerifications)

const apiKeyLookupColumns = `api_key_id, key, name, owner_id, scopes, expires_at, created_at, last_used_at, is_revoked, revoked_at, key_prefix, COALESCE(parent_api_key_id,'')`
const prefixedAPIKeyQuery = `SELECT ` + apiKeyLookupColumns + ` FROM ledgerforge.api_keys WHERE key_prefix = $1`
const legacyAPIKeyQuery = `SELECT ` + apiKeyLookupColumns + ` FROM ledgerforge.api_keys WHERE (key_prefix IS NULL OR key_prefix = '') AND is_revoked = false AND expires_at > CURRENT_TIMESTAMP`

// hashAPIKey creates a bcrypt hash of the API key for secure storage.
// bcrypt includes salt and is resistant to brute-force attacks.
func hashAPIKey(key string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(key), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// verifyAPIKey compares a plaintext key with a bcrypt hash.
func verifyAPIKey(hashedKey, plainKey string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedKey), []byte(plainKey))
	return err == nil
}

// getKeyPrefix extracts the first 16 characters of the key for efficient lookup.
func getKeyPrefix(key string) string {
	if len(key) >= 16 {
		return key[:16]
	}
	return key
}

// CreateAPIKey creates a new API key with the specified parameters and stores it in the database.
// The key is hashed using bcrypt before storage for security. The plain text key is returned ONLY during creation
// and should be displayed to the user immediately as it cannot be retrieved later.
//
// Parameters:
// - ctx: Context for managing the request lifecycle and cancellation.
// - name: A human-readable name for the API key.
// - ownerID: The ID of the user or entity that owns this API key.
// - scopes: A slice of permission scopes that this API key grants access to.
// - expiresAt: The timestamp when this API key will expire.
//
// Returns:
// - *model.APIKey: The created API key object with the PLAIN TEXT key (store this immediately, it won't be retrievable later).
// - error: An error if the API key creation fails during generation or database insertion.
func (s *Datasource) CreateAPIKey(ctx context.Context, name, ownerID string, scopes []string, expiresAt time.Time) (*model.APIKey, error) {
	// Generate the API key with plain text
	apiKey, err := model.NewAPIKey(name, ownerID, scopes, expiresAt)
	if err != nil {
		return nil, err
	}

	principal := policy.FromContext(ctx)
	if !principal.Master {
		apiKey.ParentAPIKeyID = principal.APIKeyID
	}

	// Store the plain text key to return to the user
	plainTextKey := apiKey.Key

	// Extract prefix for efficient lookup
	keyPrefix := getKeyPrefix(plainTextKey)

	// Hash the key using bcrypt before storing in database
	hashedKey, err := hashAPIKey(plainTextKey)
	if err != nil {
		return nil, fmt.Errorf("failed to hash API key: %w", err)
	}
	apiKey.Key = hashedKey

	query := `
		INSERT INTO ledgerforge.api_keys (api_key_id, key, key_prefix, name, owner_id, scopes, expires_at, created_at, last_used_at, is_revoked, parent_api_key_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11,''))
	`

	_, err = s.Conn.ExecContext(ctx, query,
		apiKey.APIKeyID,
		apiKey.Key, // This is now the bcrypt hashed key
		keyPrefix,
		apiKey.Name,
		apiKey.OwnerID,
		pq.StringArray(apiKey.Scopes),
		apiKey.ExpiresAt,
		apiKey.CreatedAt,
		apiKey.LastUsedAt,
		apiKey.IsRevoked,
		apiKey.ParentAPIKeyID,
	)
	if err != nil {
		return nil, err
	}

	// Return the API key with the PLAIN TEXT key for the user
	// This is the ONLY time they'll see this key
	apiKey.Key = plainTextKey
	return apiKey, nil
}

// GetAPIKey retrieves an API key from the database using its key string.
// The provided key's prefix is used for efficient lookup, then bcrypt verifies the full key.
// PostgreSQL is authoritative for scopes, expiry and revocation. Legacy bcrypt
// keys without key_prefix are verified and backfilled on first successful use.
// This function is typically used for authentication and authorization purposes.
//
// Parameters:
// - ctx: Context for managing the request lifecycle and cancellation.
// - key: The PLAIN TEXT API key string to authenticate.
//
// Returns:
// - *model.APIKey: The API key object if found, with an empty Key field.
// - error: Returns ErrAPIKeyNotFound if the key doesn't exist, or other database errors if the query fails.
func (s *Datasource) GetAPIKey(ctx context.Context, key string) (*model.APIKey, error) {
	// PostgreSQL remains authoritative; a stale cache must never authorize.
	candidate, foundPrefix, err := s.findAPIKey(ctx, key, false)
	if foundPrefix || (err != nil && !errors.Is(err, ErrAPIKeyNotFound)) {
		return candidate, err
	}
	if s.disableLegacyAPIKeys {
		return nil, ErrAPIKeyNotFound
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case legacyAPIKeyVerificationSlots <- struct{}{}:
		defer func() { <-legacyAPIKeyVerificationSlots }()
	default:
		return nil, ErrLegacyAPIKeyVerificationBusy
	}
	candidate, _, err = s.findAPIKey(ctx, key, true)
	return candidate, err
}

func (s *Datasource) findAPIKey(ctx context.Context, key string, legacy bool) (*model.APIKey, bool, error) {
	query := prefixedAPIKeyQuery
	var args []interface{}
	if legacy {
		query = legacyAPIKeyQuery
	} else {
		args = []interface{}{getKeyPrefix(key)}
	}
	rows, err := s.Conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = rows.Close() }()
	found := false
	for rows.Next() {
		found = true
		var candidate model.APIKey
		var scopes pq.StringArray
		var storedPrefix sql.NullString
		if err := rows.Scan(&candidate.APIKeyID, &candidate.Key, &candidate.Name, &candidate.OwnerID, &scopes,
			&candidate.ExpiresAt, &candidate.CreatedAt, &candidate.LastUsedAt, &candidate.IsRevoked, &candidate.RevokedAt, &storedPrefix, &candidate.ParentAPIKeyID); err != nil {
			return nil, found, err
		}
		candidate.Scopes = []string(scopes)
		// Also check the application clock before each verification: candidates
		// can expire after the SQL statement's snapshot was taken.
		if legacy && !candidate.IsValid() {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, found, err
		}
		if !verifyAPIKey(candidate.Key, key) {
			continue
		}
		if legacy {
			if err := rows.Close(); err != nil {
				return nil, found, err
			}
			_, err := s.Conn.ExecContext(ctx, `UPDATE ledgerforge.api_keys SET key_prefix = $1 WHERE api_key_id = $2 AND key = $3 AND (key_prefix IS NULL OR key_prefix = '')`, getKeyPrefix(key), candidate.APIKeyID, candidate.Key)
			if err != nil {
				return nil, found, err
			}
		}
		candidate.Key = ""
		return &candidate, found, nil
	}
	if err := rows.Err(); err != nil {
		return nil, found, err
	}
	return nil, found, ErrAPIKeyNotFound
}

func (s *Datasource) warnLegacyAPIKeys(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var count int64
	err := s.Conn.QueryRowContext(ctx, `SELECT count(*) FROM ledgerforge.api_keys WHERE key_prefix IS NULL OR key_prefix = ''`).Scan(&count)
	if err != nil {
		logrus.WithError(err).Warn("could not count legacy API keys at startup")
		return
	}
	if count > 0 {
		logrus.WithFields(logrus.Fields{"legacy_api_key_count": count, "allow_legacy_api_keys": !s.disableLegacyAPIKeys}).Warn("rotate legacy API keys, revoke the old keys, then set server.allow_legacy_api_keys=false to disable compatibility verification")
	}
}

// RevokeAPIKey marks an API key as revoked in the database, preventing its future use.
// The function updates the is_revoked flag to true and sets the revoked_at timestamp.
// Authentication does not trust cached key state.
// Only the owner of the API key can revoke it.
//
// Parameters:
// - ctx: Context for managing the request lifecycle and cancellation.
// - id: The unique identifier of the API key to revoke.
// - ownerID: The ID of the owner, used to ensure only the owner can revoke their keys.
//
// Returns:
//   - error: Returns ErrAPIKeyNotFound if the key doesn't exist or doesn't belong to the owner,
//     or other database errors if the update operation fails.
func (s *Datasource) RevokeAPIKey(ctx context.Context, id, ownerID string) error {
	// Update the API key to mark it as revoked
	updateQuery := `
		UPDATE ledgerforge.api_keys
		SET is_revoked = true, revoked_at = $1
		WHERE api_key_id = $2 AND owner_id = $3
	`

	result, err := s.Conn.ExecContext(ctx, updateQuery, time.Now(), id, ownerID)
	if err != nil {
		return err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rows == 0 {
		return ErrAPIKeyNotFound
	}

	return nil
}

// UpdateLastUsed updates the last_used_at timestamp for an API key to the current time.
// This function is typically called during authentication to track API key usage patterns.
// The timestamp is persisted for auditing purposes.
//
// Parameters:
// - ctx: Context for managing the request lifecycle and cancellation.
// - id: The unique identifier of the API key to update.
//
// Returns:
// - error: An error if the database update operation fails.
func (s *Datasource) UpdateLastUsed(ctx context.Context, id string) error {
	query := `
		UPDATE ledgerforge.api_keys
		SET last_used_at = $1
		WHERE api_key_id = $2
	`

	_, err := s.Conn.ExecContext(ctx, query, time.Now(), id)
	return err
}

// ListAPIKeys retrieves all API keys belonging to a specific owner from the database.
// The results are ordered by creation date in descending order (newest first).
// The Key field is empty; stored hashes never leave the authentication layer.
//
// Parameters:
// - ctx: Context for managing the request lifecycle and cancellation.
// - ownerID: The ID of the owner whose API keys should be retrieved.
//
// Returns:
// - []*model.APIKey: A slice of API key objects belonging to the specified owner.
// - error: An error if the database query fails or if there are issues scanning the results.
func (s *Datasource) ListAPIKeys(ctx context.Context, ownerID string) ([]*model.APIKey, error) {
	query := `
		SELECT api_key_id, key, name, owner_id, scopes, expires_at, created_at, last_used_at, is_revoked, revoked_at, COALESCE(parent_api_key_id,'')
		FROM ledgerforge.api_keys
		WHERE owner_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.Conn.QueryContext(ctx, query, ownerID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var apiKeys []*model.APIKey
	for rows.Next() {
		apiKey := &model.APIKey{}
		var scopes pq.StringArray
		err := rows.Scan(
			&apiKey.APIKeyID,
			&apiKey.Key,
			&apiKey.Name,
			&apiKey.OwnerID,
			&scopes,
			&apiKey.ExpiresAt,
			&apiKey.CreatedAt,
			&apiKey.LastUsedAt,
			&apiKey.IsRevoked,
			&apiKey.RevokedAt,
			&apiKey.ParentAPIKeyID,
		)
		apiKey.Scopes = []string(scopes)
		if err != nil {
			return nil, err
		}
		apiKey.Key = ""
		apiKeys = append(apiKeys, apiKey)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return apiKeys, nil
}
