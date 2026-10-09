/*
Copyright 2026 LedgerForge Contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package database

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/devaccuracy/ledgerforge/model"
	"github.com/stretchr/testify/require"
)

// A stale cache models a cache fill that finishes after revocation and failed
// invalidation. Authentication must not use it even though bcrypt would match.
type staleAPIKeyCache struct {
	mu  sync.Mutex
	key model.APIKey
}

func (c *staleAPIKeyCache) Set(_ context.Context, _ string, value interface{}, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.key = *value.(*model.APIKey)
	return nil
}
func (c *staleAPIKeyCache) Get(_ context.Context, _ string, value interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	*value.(*model.APIKey) = c.key
	return nil
}
func (c *staleAPIKeyCache) Delete(context.Context, string) error {
	return errors.New("cache unavailable")
}

func TestAPIKeyRevocationAuthoritativeWithConcurrentStaleCache(t *testing.T) {
	ds := openRealTestDB(t)
	ctx := context.Background()
	key, err := ds.CreateAPIKey(ctx, "revocation-test", model.GenerateUUIDWithSuffix("owner"), []string{"ledgers:read"}, time.Now().Add(time.Hour))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = ds.Conn.ExecContext(ctx, "DELETE FROM ledgerforge.api_keys WHERE api_key_id = $1", key.APIKeyID)
	})
	hash, err := hashAPIKey(key.Key)
	require.NoError(t, err)
	stale := *key
	stale.Key = hash
	cache := &staleAPIKeyCache{key: stale}
	ds.Cache = cache
	var wg sync.WaitGroup
	start := make(chan struct{})
	revoked := make(chan struct{})
	failures := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			// This lookup can overlap revocation; only requests starting after it
			// completes are required to observe the revoked state.
			_, _ = ds.GetAPIKey(ctx, key.Key)
			<-revoked
			_ = cache.Set(ctx, "api_key:hash:"+getKeyPrefix(key.Key), &stale, time.Minute)
			for j := 0; j < 3; j++ {
				got, err := ds.GetAPIKey(ctx, key.Key)
				if err != nil {
					failures <- err
					return
				}
				if got.IsValid() {
					failures <- errors.New("revoked key authenticated from stale cache")
					return
				}
				if got.Key != "" {
					failures <- errors.New("stored hash escaped authentication")
					return
				}
			}
		}()
	}
	close(start)
	require.NoError(t, ds.RevokeAPIKey(ctx, key.APIKeyID, key.OwnerID))
	close(revoked)
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
}

func TestAPIKeyLegacyBcryptWithoutPrefix(t *testing.T) {
	ds := openRealTestDB(t)
	ctx := context.Background()
	key, err := ds.CreateAPIKey(ctx, "legacy-test", model.GenerateUUIDWithSuffix("owner"), []string{"ledgers:read"}, time.Now().Add(time.Hour))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = ds.Conn.ExecContext(ctx, "DELETE FROM ledgerforge.api_keys WHERE api_key_id = $1", key.APIKeyID)
	})
	_, err = ds.Conn.ExecContext(ctx, "UPDATE ledgerforge.api_keys SET key_prefix = NULL WHERE api_key_id = $1", key.APIKeyID)
	require.NoError(t, err)
	got, err := ds.GetAPIKey(ctx, key.Key)
	require.NoError(t, err)
	require.Equal(t, key.APIKeyID, got.APIKeyID)
	require.Empty(t, got.Key)
	var prefix string
	require.NoError(t, ds.Conn.QueryRowContext(ctx, "SELECT key_prefix FROM ledgerforge.api_keys WHERE api_key_id = $1", key.APIKeyID).Scan(&prefix))
	require.Equal(t, getKeyPrefix(key.Key), prefix)
	listed, err := ds.ListAPIKeys(ctx, key.OwnerID)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Empty(t, listed[0].Key)
}
