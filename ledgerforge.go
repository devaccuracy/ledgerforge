/*
Copyright 2024 Blnk Finance Authors.

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

package ledgerforge

import (
	"embed"
	"net/http"
	"time"

	"github.com/hibiken/asynq"

	"github.com/devaccuracy/ledgerforge/config"
	"github.com/devaccuracy/ledgerforge/database"
	"github.com/devaccuracy/ledgerforge/internal/cache"
	"github.com/devaccuracy/ledgerforge/internal/hooks"
	"github.com/devaccuracy/ledgerforge/internal/hotpairs"
	"github.com/devaccuracy/ledgerforge/internal/notification"
	redis_db "github.com/devaccuracy/ledgerforge/internal/redis-db"
	"github.com/devaccuracy/ledgerforge/internal/search"
	"github.com/devaccuracy/ledgerforge/internal/tokenization"

	"github.com/devaccuracy/ledgerforge/model"
	"github.com/redis/go-redis/v9"
)

// LedgerForge represents the main struct for the LedgerForge application.
type LedgerForge struct {
	queue       *Queue
	search      *search.TypesenseClient
	redis       redis.UniversalClient
	asynqClient *asynq.Client
	datasource  database.IDataSource
	bt          *model.BalanceTracker
	tokenizer   *tokenization.TokenizationService
	httpClient  *http.Client
	Hooks       hooks.HookManager
	config      *config.Configuration
	cache       cache.Cache
	hotPairs    *hotpairs.Manager
}

const (
	GeneralLedgerID = "general_ledger_id"
)

//go:embed sql/*.sql
var SQLFiles embed.FS

// initializeRedisClients sets up both the Redis client and Asynq client
func initializeRedisClients(config *config.Configuration) (redis.UniversalClient, *asynq.Client, error) {
	redisClient, err := redis_db.NewRedisClient([]string{config.Redis.Dns}, config.Redis.SkipTLSVerify, &redis_db.PoolConfig{
		PoolSize:     config.Redis.PoolSize,
		MinIdleConns: config.Redis.MinIdleConns,
	})
	if err != nil {
		return nil, nil, err
	}

	redisOption, err := redis_db.ParseRedisURL(config.Redis.Dns, config.Redis.SkipTLSVerify)
	if err != nil {
		return nil, nil, err
	}

	asynqClient := asynq.NewClient(asynq.RedisClientOpt{
		Addr:      redisOption.Addr,
		Password:  redisOption.Password,
		DB:        redisOption.DB,
		TLSConfig: redisOption.TLSConfig,
		PoolSize:  config.Redis.PoolSize,
	})

	return redisClient.Client(), asynqClient, nil
}

// initializeTokenizationService creates and configures the tokenization service
func initializeTokenizationService(config *config.Configuration) *tokenization.TokenizationService {
	if config.TokenizationSecret == "" {
		return tokenization.NewTokenizationService(nil)
	}

	key := []byte(config.TokenizationSecret)
	return tokenization.NewTokenizationService(key)
}

// initializeHTTPClient creates and configures the HTTP client for webhook requests
func initializeHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}

// NewLedgerForge initializes a new instance of LedgerForge with the provided database datasource.
// It fetches the configuration, initializes Redis client, balance tracker, queue, and search client.
//
// Parameters:
// - db database.IDataSource: The datasource for database operations.
//
// Returns:
// - *LedgerForge: A pointer to the newly created LedgerForge instance.
// - error: An error if any of the initialization steps fail.
func NewLedgerForge(db database.IDataSource) (*LedgerForge, error) {
	configuration, err := config.Fetch()
	if err != nil {
		return nil, err
	}

	redisClient, asynqClient, err := initializeRedisClients(configuration)
	if err != nil {
		return nil, err
	}

	bt := NewBalanceTracker()
	hotPairManager := hotpairs.NewManager(redisClient, hotpairs.Config{
		Enabled:                 configuration.Queue.EnableHotLane,
		HotQueueName:            configuration.Queue.HotQueueName,
		HotPairTTL:              configuration.Queue.HotPairTTL,
		LockContentionThreshold: configuration.Queue.HotPairLockContentionThreshold,
	})
	newQueue := NewQueue(configuration, asynqClient)
	newSearch := search.NewTypesenseClient(configuration.TypeSenseKey, []string{configuration.TypeSense.Dns})
	hookManager := hooks.NewHookManager(redisClient, asynqClient)
	tokenizer := initializeTokenizationService(configuration)
	httpClient := initializeHTTPClient()

	newCache := cache.NewCacheWithClient(redisClient)

	b := &LedgerForge{
		datasource:  db,
		bt:          bt,
		queue:       newQueue,
		redis:       redisClient,
		asynqClient: asynqClient,
		search:      newSearch,
		tokenizer:   tokenizer,
		httpClient:  httpClient,
		Hooks:       hookManager,
		config:      configuration,
		cache:       newCache,
		hotPairs:    hotPairManager,
	}

	notification.RegisterWebhookSender(func(event string, payload interface{}) error {
		return b.SendWebhook(NewWebhook{
			Event:   event,
			Payload: payload,
		})
	})

	return b, nil
}

// Close properly closes all connections and resources used by the LedgerForge instance.
func (b *LedgerForge) Close() error {
	if b.asynqClient != nil {
		return b.asynqClient.Close()
	}
	return nil
}

// Config returns the cached configuration for the LedgerForge instance.
// Falls back to config.Fetch() if not initialized (for backward compatibility with tests).
func (b *LedgerForge) Config() *config.Configuration {
	if b.config != nil {
		return b.config
	}
	cfg, err := config.Fetch()
	if err != nil {
		return &config.Configuration{}
	}
	return cfg
}

func (b *LedgerForge) GetSearchClient() *search.TypesenseClient {
	return b.search
}

func (b *LedgerForge) GetDataSource() database.IDataSource {
	return b.datasource
}
