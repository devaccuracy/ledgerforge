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

package config

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/sirupsen/logrus"
)

// Default constants
const (
	DEFAULT_PORT               = "5001"
	DEFAULT_CLEANUP_SEC        = 10800 // 3 hours in seconds
	DEFAULT_TYPESENSE_KEY      = "ledgerforge-api-key"
	DEFAULT_MONITORING_PORT    = "5004"
	DEFAULT_MAX_UPLOAD_SIZE_MB = 256 // caps reconciliation file uploads
	// DEFAULT_MAX_REQUEST_BODY_SIZE_MB caps non-upload request bodies so a large
	// POST can't exhaust memory before a handler (or the auth middleware) reads it.
	DEFAULT_MAX_REQUEST_BODY_SIZE_MB = 5
	// DEFAULT_UPLOAD_URL_TIMEOUT_SEC caps how long a URL-based reconciliation
	// upload may spend fetching the remote body, preventing a slow or stalled
	// upstream from hanging the handler.
	DEFAULT_UPLOAD_URL_TIMEOUT_SEC = 30
)

// Default values for different configurations
var (
	defaultTransaction = TransactionConfig{
		BatchSize:                  1000,
		MaxQueueSize:               1000,
		MaxWorkers:                 10,
		LockDuration:               5 * time.Minute,
		LockWaitTimeout:            3 * time.Second,
		IndexQueuePrefix:           "transactions",
		EnableCoalescing:           true,
		EnableQueuedChecks:         false,
		DisableBatchReferenceCheck: false,
	}

	defaultReconciliation = ReconciliationConfig{
		DefaultStrategy:  "one_to_one",
		ProgressInterval: 100,
		MaxRetries:       3,
		RetryDelay:       5 * time.Second,
	}

	defaultTypeSense = TypeSenseConfig{
		BackpressureRetryInterval: 5 * time.Second,
	}

	defaultQueue = QueueConfig{
		TransactionQueue:                "new:transaction",
		WebhookQueue:                    "new:webhook",
		IndexQueue:                      "new:index",
		InflightExpiryQueue:             "new:inflight-expiry",
		InflightCommitQueue:             "new:inflight-commit",
		NumberOfQueues:                  20,
		EnableHotLane:                   false,
		HotQueueName:                    "hot_transactions",
		HotQueueConcurrency:             1,
		HotPairTTL:                      5 * time.Minute,
		HotPairLockContentionThreshold:  3,
		RejectLockContentionImmediately: false,
		MaxRetryAttempts:                5,
		MonitoringPort:                  DEFAULT_MONITORING_PORT,
		WebhookConcurrency:              20,
		TransactionWorkerConcurrency:    4,
		EnableBackpressure:              true,
		BackpressureMemoryPercent:       85,
		BackpressureCheckInterval:       500 * time.Millisecond,
	}

	defaultRedis = RedisConfig{
		PoolSize:        100,
		MinIdleConns:    20,
		ConnMaxIdleTime: 2 * time.Minute,
	}

	defaultDatabase = DataSourceConfig{
		MaxOpenConns:    50,
		MaxIdleConns:    25,
		ConnMaxLifetime: 30 * time.Minute,
		ConnMaxIdleTime: 5 * time.Minute,
	}
)

var ConfigStore atomic.Value

type ServerConfig struct {
	// AllowLegacyAPIKeys enables the bounded compatibility lookup for active
	// bcrypt keys without key_prefix. Default true; disable after rotating old keys.
	AllowLegacyAPIKeys   *bool  `json:"allow_legacy_api_keys" envconfig:"LEDGERFORGE_SERVER_ALLOW_LEGACY_API_KEYS"`
	SSL                  bool   `json:"ssl"                      envconfig:"LEDGERFORGE_SERVER_SSL"`
	CertStoragePath      string `json:"cert_storage_path"        envconfig:"LEDGERFORGE_CERT_STORAGE_PATH"`
	Secure               bool   `json:"secure"                   envconfig:"LEDGERFORGE_SERVER_SECURE"`
	SecretKey            string `json:"secret_key"               envconfig:"LEDGERFORGE_SERVER_SECRET_KEY"`
	Domain               string `json:"domain"                   envconfig:"LEDGERFORGE_SERVER_SSL_DOMAIN"`
	Email                string `json:"ssl_email"                envconfig:"LEDGERFORGE_SERVER_SSL_EMAIL"`
	Port                 string `json:"port"                     envconfig:"LEDGERFORGE_SERVER_PORT"`
	MetricsBearerToken   string `json:"metrics_bearer_token"     envconfig:"LEDGERFORGE_METRICS_BEARER_TOKEN"`
	MaxUploadSizeMB      int64  `json:"max_upload_size_mb"       envconfig:"LEDGERFORGE_SERVER_MAX_UPLOAD_SIZE_MB"`
	MaxRequestBodySizeMB int64  `json:"max_request_body_size_mb" envconfig:"LEDGERFORGE_SERVER_MAX_REQUEST_BODY_SIZE_MB"`

	// UploadWhitelist is a comma-separated list of exact hostnames permitted as
	// targets for URL-based reconciliation uploads (LEDGERFORGE_UPLOAD_DOMAIN_WHITELIST).
	// Empty/unset means deny-by-default: every URL upload is rejected. Listing a
	// bare IP literal or "localhost" is unsafe and defeats the SSRF guard.
	UploadDomainWhitelist string `json:"upload_whitelist"       envconfig:"LEDGERFORGE_UPLOAD_DOMAIN_WHITELIST"`
	// UploadURLTimeoutSec caps the HTTP GET issued for a URL-based upload
	// (LEDGERFORGE_UPLOAD_URL_TIMEOUT_SEC). Defaults to DEFAULT_UPLOAD_URL_TIMEOUT_SEC.
	UploadURLTimeoutSec int `json:"upload_url_timeout_sec" envconfig:"LEDGERFORGE_UPLOAD_URL_TIMEOUT_SEC"`
}

type DataSourceConfig struct {
	Dns             string        `json:"dns"                envconfig:"LEDGERFORGE_DATA_SOURCE_DNS"`
	MaxOpenConns    int           `json:"max_open_conns"     envconfig:"LEDGERFORGE_DATABASE_MAX_OPEN_CONNS"`
	MaxIdleConns    int           `json:"max_idle_conns"     envconfig:"LEDGERFORGE_DATABASE_MAX_IDLE_CONNS"`
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime"  envconfig:"LEDGERFORGE_DATABASE_CONN_MAX_LIFETIME"`
	ConnMaxIdleTime time.Duration `json:"conn_max_idle_time" envconfig:"LEDGERFORGE_DATABASE_CONN_MAX_IDLE_TIME"`
}

type RedisConfig struct {
	Dns           string `json:"dns"             envconfig:"LEDGERFORGE_REDIS_DNS"`
	SkipTLSVerify bool   `json:"skip_tls_verify" envconfig:"LEDGERFORGE_REDIS_SKIP_TLS_VERIFY"`
	PoolSize      int    `json:"pool_size"       envconfig:"LEDGERFORGE_REDIS_POOL_SIZE"`
	MinIdleConns  int    `json:"min_idle_conns"  envconfig:"LEDGERFORGE_REDIS_MIN_IDLE_CONNS"`
	// ConnMaxIdleTime must stay below any server or proxy idle timeout
	// (managed Redis/Valkey services commonly use 300s) so pooled connections
	// are recycled before the remote end closes them ("write: broken pipe").
	ConnMaxIdleTime time.Duration `json:"conn_max_idle_time" envconfig:"LEDGERFORGE_REDIS_CONN_MAX_IDLE_TIME"`
}

type TypeSenseConfig struct {
	Dns                       string        `json:"dns"                             envconfig:"LEDGERFORGE_TYPESENSE_DNS"`
	BackpressureRetryInterval time.Duration `json:"backpressure_retry_interval"     envconfig:"LEDGERFORGE_TYPESENSE_BACKPRESSURE_RETRY_INTERVAL"`
}

type AccountGenerationHttpService struct {
	Url     string `json:"url"`
	Timeout int    `json:"timeout"`
	Headers struct {
		Authorization string `json:"Authorization"`
	} `json:"headers"`
}
type AccountNumberGenerationConfig struct {
	EnableAutoGeneration bool                         `json:"enable_auto_generation"`
	HttpService          AccountGenerationHttpService `json:"http_service"`
}

// RateLimitConfig preserves the legacy defaults: 5,000,000 requests/second
// and a 10,000,000 burst. Deployments should set explicit workload-based limits.
type RateLimitConfig struct {
	RequestsPerSecond  *float64 `json:"requests_per_second"  envconfig:"LEDGERFORGE_RATE_LIMIT_RPS"`
	Burst              *int     `json:"burst"                envconfig:"LEDGERFORGE_RATE_LIMIT_BURST"`
	CleanupIntervalSec *int     `json:"cleanup_interval_sec" envconfig:"LEDGERFORGE_RATE_LIMIT_CLEANUP_INTERVAL_SEC"`
}

type SlackWebhook struct {
	WebhookUrl string `json:"webhook_url" envconfig:"LEDGERFORGE_SLACK_WEBHOOK_URL"`
}

type WebhookConfig struct {
	// SigningSecret is shared by notifications and transaction hooks. When unset,
	// Server.SecretKey is the legacy fallback. With neither set, delivery is unsigned.
	SigningSecret string `json:"signing_secret" envconfig:"LEDGERFORGE_WEBHOOK_SIGNING_SECRET"`
	// BlockPrivateDestinations rejects non-global special-purpose and translation
	// addresses at registration and connection time. Default false preserves local receivers.
	BlockPrivateDestinations bool `json:"block_private_destinations" envconfig:"LEDGERFORGE_WEBHOOK_BLOCK_PRIVATE_DESTINATIONS"`
	// TimeoutSec defaults to 30 seconds and is capped at 120 seconds.
	TimeoutSec int               `json:"timeout_sec" envconfig:"LEDGERFORGE_WEBHOOK_TIMEOUT_SEC"`
	Url        string            `json:"url"     envconfig:"LEDGERFORGE_WEBHOOK_URL"`
	Headers    map[string]string `json:"headers" envconfig:"LEDGERFORGE_WEBHOOK_HEADERS"`
}

// WebhookSigningSecret selects the dedicated secret before the legacy API secret.
func (cnf *Configuration) WebhookSigningSecret() string {
	if cnf.Notification.Webhook.SigningSecret != "" {
		return cnf.Notification.Webhook.SigningSecret
	}
	return cnf.Server.SecretKey
}

type Notification struct {
	Slack   SlackWebhook  `json:"slack"`
	Webhook WebhookConfig `json:"webhook"`
}

type TransactionConfig struct {
	BatchSize                  int             `json:"batch_size"                    envconfig:"LEDGERFORGE_TRANSACTION_BATCH_SIZE"`
	MaxQueueSize               int             `json:"max_queue_size"                envconfig:"LEDGERFORGE_TRANSACTION_MAX_QUEUE_SIZE"`
	MaxWorkers                 int             `json:"max_workers"                   envconfig:"LEDGERFORGE_TRANSACTION_MAX_WORKERS"`
	LockDuration               time.Duration   `json:"lock_duration"                 envconfig:"LEDGERFORGE_TRANSACTION_LOCK_DURATION"`
	LockWaitTimeout            time.Duration   `json:"lock_wait_timeout"             envconfig:"LEDGERFORGE_TRANSACTION_LOCK_WAIT_TIMEOUT"`
	IndexQueuePrefix           string          `json:"index_queue_prefix"            envconfig:"LEDGERFORGE_TRANSACTION_INDEX_QUEUE_PREFIX"`
	EnableCoalescing           bool            `json:"enable_coalescing"             envconfig:"LEDGERFORGE_TRANSACTION_ENABLE_COALESCING"`
	EnableQueuedChecks         bool            `json:"enable_queued_checks"          envconfig:"LEDGERFORGE_TRANSACTION_ENABLE_QUEUED_CHECKS"`
	DisableBatchReferenceCheck bool            `json:"disable_batch_reference_check" envconfig:"LEDGERFORGE_TRANSACTION_DISABLE_BATCH_REFERENCE_CHECK"`
	HashChain                  HashChainConfig `json:"hash_chain"`
}

type ReconciliationConfig struct {
	DefaultStrategy  string        `json:"default_strategy"  envconfig:"LEDGERFORGE_RECONCILIATION_DEFAULT_STRATEGY"`
	ProgressInterval int           `json:"progress_interval" envconfig:"LEDGERFORGE_RECONCILIATION_PROGRESS_INTERVAL"`
	MaxRetries       int           `json:"max_retries"       envconfig:"LEDGERFORGE_RECONCILIATION_MAX_RETRIES"`
	RetryDelay       time.Duration `json:"retry_delay"       envconfig:"LEDGERFORGE_RECONCILIATION_RETRY_DELAY"`
}

type QueueConfig struct {
	TransactionQueue                string        `json:"transaction_queue"                  envconfig:"LEDGERFORGE_QUEUE_TRANSACTION"`
	WebhookQueue                    string        `json:"webhook_queue"                      envconfig:"LEDGERFORGE_QUEUE_WEBHOOK"`
	IndexQueue                      string        `json:"index_queue"                        envconfig:"LEDGERFORGE_QUEUE_INDEX"`
	InflightExpiryQueue             string        `json:"inflight_expiry_queue"              envconfig:"LEDGERFORGE_QUEUE_INFLIGHT_EXPIRY"`
	InflightCommitQueue             string        `json:"inflight_commit_queue"              envconfig:"LEDGERFORGE_QUEUE_INFLIGHT_COMMIT"`
	NumberOfQueues                  int           `json:"number_of_queues"                   envconfig:"LEDGERFORGE_QUEUE_NUMBER_OF_QUEUES"`
	EnableHotLane                   bool          `json:"enable_hot_lane"                    envconfig:"LEDGERFORGE_QUEUE_ENABLE_HOT_LANE"`
	HotQueueName                    string        `json:"hot_queue_name"                     envconfig:"LEDGERFORGE_QUEUE_HOT_QUEUE_NAME"`
	HotQueueConcurrency             int           `json:"hot_queue_concurrency"              envconfig:"LEDGERFORGE_QUEUE_HOT_QUEUE_CONCURRENCY"`
	HotPairTTL                      time.Duration `json:"hot_pair_ttl"                       envconfig:"LEDGERFORGE_QUEUE_HOT_PAIR_TTL"`
	HotPairLockContentionThreshold  int           `json:"hot_pair_lock_contention_threshold" envconfig:"LEDGERFORGE_QUEUE_HOT_PAIR_LOCK_CONTENTION_THRESHOLD"`
	RejectLockContentionImmediately bool          `json:"reject_lock_contention_immediately" envconfig:"LEDGERFORGE_QUEUE_REJECT_LOCK_CONTENTION_IMMEDIATELY"`
	InsufficientFundRetries         bool          `json:"insufficient_fund_retries"          envconfig:"LEDGERFORGE_QUEUE_INSUFFICIENT_FUND_RETRIES"`
	MaxRetryAttempts                int           `json:"max_retry_attempts"                 envconfig:"LEDGERFORGE_QUEUE_MAX_RETRY_ATTEMPTS"`
	MonitoringPort                  string        `json:"monitoring_port"                    envconfig:"LEDGERFORGE_QUEUE_MONITORING_PORT"`
	WebhookConcurrency              int           `json:"webhook_concurrency"                envconfig:"LEDGERFORGE_QUEUE_WEBHOOK_CONCURRENCY"`
	TransactionWorkerConcurrency    int           `json:"transaction_worker_concurrency"     envconfig:"LEDGERFORGE_QUEUE_TRANSACTION_WORKER_CONCURRENCY"`
	EnableBackpressure              bool          `json:"enable_backpressure"                envconfig:"LEDGERFORGE_QUEUE_ENABLE_BACKPRESSURE"`
	BackpressureMemoryPercent       float64       `json:"backpressure_memory_percent"        envconfig:"LEDGERFORGE_QUEUE_BACKPRESSURE_MEMORY_PERCENT"`
	BackpressureMaxPendingTasks     int           `json:"backpressure_max_pending_tasks"     envconfig:"LEDGERFORGE_QUEUE_BACKPRESSURE_MAX_PENDING_TASKS"`
	BackpressureCheckInterval       time.Duration `json:"backpressure_check_interval"        envconfig:"LEDGERFORGE_QUEUE_BACKPRESSURE_CHECK_INTERVAL"`
}

type Configuration struct {
	ProjectName             string                        `json:"project_name"              envconfig:"LEDGERFORGE_PROJECT_NAME"`
	BackupDir               string                        `json:"backup_dir"                envconfig:"LEDGERFORGE_BACKUP_DIR"`
	AwsAccessKeyId          string                        `json:"aws_access_key_id"         envconfig:"LEDGERFORGE_AWS_ACCESS_KEY_ID"`
	S3Endpoint              string                        `json:"s3_endpoint"               envconfig:"LEDGERFORGE_S3_ENDPOINT"`
	AwsSecretAccessKey      string                        `json:"aws_secret_access_key"     envconfig:"LEDGERFORGE_AWS_SECRET_ACCESS_KEY"`
	S3BucketName            string                        `json:"s3_bucket_name"            envconfig:"LEDGERFORGE_S3_BUCKET_NAME"`
	S3Region                string                        `json:"s3_region"                 envconfig:"LEDGERFORGE_S3_REGION"`
	Server                  ServerConfig                  `json:"server"`
	DataSource              DataSourceConfig              `json:"data_source"`
	Redis                   RedisConfig                   `json:"redis"`
	TypeSense               TypeSenseConfig               `json:"typesense"`
	TypeSenseKey            string                        `json:"type_sense_key"            envconfig:"LEDGERFORGE_TYPESENSE_KEY"`
	TokenizationSecret      string                        `json:"tokenization_secret"       envconfig:"LEDGERFORGE_TOKENIZATION_SECRET"`
	AccountNumberGeneration AccountNumberGenerationConfig `json:"account_number_generation"`
	Notification            Notification                  `json:"notification"`
	RateLimit               RateLimitConfig               `json:"rate_limit"`
	EnableTelemetry         bool                          `json:"enable_telemetry" envconfig:"LEDGERFORGE_ENABLE_TELEMETRY"`
	TelemetryKey            string                        `json:"telemetry_key" envconfig:"LEDGERFORGE_TELEMETRY_KEY"`
	TelemetryEndpoint       string                        `json:"telemetry_endpoint" envconfig:"LEDGERFORGE_TELEMETRY_ENDPOINT"`
	EnableObservability     bool                          `json:"enable_observability" envconfig:"LEDGERFORGE_ENABLE_OBSERVABILITY"`
	MonitoringDSN           string                        `json:"monitoring_dsn" envconfig:"LEDGERFORGE_MONITORING_DSN"`
	Transaction             TransactionConfig             `json:"transaction"`
	Reconciliation          ReconciliationConfig          `json:"reconciliation"`
	Queue                   QueueConfig                   `json:"queue"`
}

// HashChainConfig controls the background hash-chainer that seals transaction
// history into a tamper-evident chain. It lives under transaction config and is
// disabled by default.
type HashChainConfig struct {
	Enabled       bool          `json:"enabled"        envconfig:"LEDGERFORGE_TRANSACTION_HASHCHAIN_ENABLED"`
	PollInterval  time.Duration `json:"poll_interval"  envconfig:"LEDGERFORGE_TRANSACTION_HASHCHAIN_POLL_INTERVAL"`
	BatchSize     int           `json:"batch_size"     envconfig:"LEDGERFORGE_TRANSACTION_HASHCHAIN_BATCH_SIZE"`
	TrailingDelay time.Duration `json:"trailing_delay" envconfig:"LEDGERFORGE_TRANSACTION_HASHCHAIN_TRAILING_DELAY"`
}

func (cnf *Configuration) RemoteMonitoringDSN() string {
	if strings.TrimSpace(cnf.MonitoringDSN) != "" {
		return cnf.MonitoringDSN
	}
	return ""
}

func loadConfigFromFile(file string) error {
	var cnf Configuration
	_, err := os.Stat(file)
	if err == nil {
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		err = json.NewDecoder(f).Decode(&cnf)
		if err != nil {
			return err
		}

	} else if errors.Is(err, os.ErrNotExist) {
		logrus.Info("config json not passed, will use env variables")
	}

	// override config from environment variables
	err = envconfig.Process("ledgerforge", &cnf)
	if err != nil {
		return err
	}

	err = cnf.validateAndAddDefaults()
	if err != nil {
		return err
	}

	ConfigStore.Store(&cnf)
	return err
}

func InitConfig(configFile string) error {
	logger()
	return loadConfigFromFile(configFile)
}

func Fetch() (*Configuration, error) {
	config := ConfigStore.Load()
	c, ok := config.(*Configuration)
	if !ok {
		return nil, errors.New("config not loaded from file: create a JSON file called ledgerforge.json with your config")
	}
	return c, nil
}

func (cnf *Configuration) validateAndAddDefaults() error {
	if err := cnf.validateRequiredFields(); err != nil {
		return err
	}

	cnf.setDefaultValues()
	cnf.trimWhitespace()
	cnf.setupRateLimiting()

	if !cnf.Server.Secure {
		logrus.Warn(
			"SECURITY: server.secure is false — API authentication is DISABLED. Do not use this configuration in production.",
		)
	}

	// Validate tokenization secret length (AES-256 requires 32 bytes)
	if len(cnf.TokenizationSecret) > 0 && len(cnf.TokenizationSecret) != 32 {
		logrus.Warn("tokenization secret should be 32 bytes for AES-256 encryption")
	}

	return nil
}

func (cnf *Configuration) validateRequiredFields() error {
	if cnf.DataSource.Dns == "" {
		return errors.New("data source DNS is required")
	}

	if cnf.Redis.Dns == "" {
		return errors.New("redis DNS is required")
	}

	return nil
}

func (cnf *Configuration) setDefaultValues() {
	// Project defaults
	if cnf.ProjectName == "" {
		cnf.ProjectName = "LedgerForge Server"
		logrus.Warn("project name is empty, setting default name")
	}

	// Server defaults
	if cnf.Server.AllowLegacyAPIKeys == nil {
		allowed := true
		cnf.Server.AllowLegacyAPIKeys = &allowed
	}
	if cnf.Server.Port == "" {
		cnf.Server.Port = DEFAULT_PORT
		logrus.WithField("port", DEFAULT_PORT).Warn("port not specified in config, setting default")
	}

	if cnf.Server.MaxUploadSizeMB <= 0 {
		cnf.Server.MaxUploadSizeMB = DEFAULT_MAX_UPLOAD_SIZE_MB
	}

	if cnf.Server.MaxRequestBodySizeMB <= 0 {
		cnf.Server.MaxRequestBodySizeMB = DEFAULT_MAX_REQUEST_BODY_SIZE_MB
	}

	if cnf.Server.UploadURLTimeoutSec <= 0 {
		cnf.Server.UploadURLTimeoutSec = DEFAULT_UPLOAD_URL_TIMEOUT_SEC
	}

	if cnf.TypeSenseKey == "" {
		cnf.TypeSenseKey = DEFAULT_TYPESENSE_KEY
	}

	// Set module defaults
	cnf.setRedisDefaults()
	cnf.setDatabaseDefaults()
	cnf.setTransactionDefaults()
	cnf.setReconciliationDefaults()
	cnf.setTypeSenseDefaults()
	cnf.setQueueDefaults()
	cnf.setHashChainDefaults()

	if cnf.EnableTelemetry {
		logrus.Info("telemetry enabled")
	} else {
		logrus.Info("telemetry disabled")
	}

	if cnf.EnableObservability {
		logrus.Info("observability enabled")
	} else {
		logrus.Info("observability disabled")
	}
}

func (cnf *Configuration) setHashChainDefaults() {
	// Enabled stays false unless explicitly turned on.
	if cnf.Transaction.HashChain.PollInterval <= 0 {
		cnf.Transaction.HashChain.PollInterval = 5 * time.Second
	}
	if cnf.Transaction.HashChain.BatchSize <= 0 {
		cnf.Transaction.HashChain.BatchSize = 1000
	}
	if cnf.Transaction.HashChain.TrailingDelay <= 0 {
		cnf.Transaction.HashChain.TrailingDelay = 30 * time.Second
	}
}

func (cnf *Configuration) setTransactionDefaults() {
	if cnf.Transaction.BatchSize == 0 {
		cnf.Transaction.BatchSize = defaultTransaction.BatchSize
	}
	if cnf.Transaction.MaxQueueSize == 0 {
		cnf.Transaction.MaxQueueSize = defaultTransaction.MaxQueueSize
	}
	if cnf.Transaction.MaxWorkers == 0 {
		cnf.Transaction.MaxWorkers = defaultTransaction.MaxWorkers
	}
	if cnf.Transaction.LockDuration == 0 {
		cnf.Transaction.LockDuration = defaultTransaction.LockDuration
	} else if cnf.Transaction.LockDuration > 0 && cnf.Transaction.LockDuration < time.Second {
		cnf.Transaction.LockDuration = cnf.Transaction.LockDuration * time.Second
	}
	if cnf.Transaction.LockWaitTimeout == 0 {
		cnf.Transaction.LockWaitTimeout = defaultTransaction.LockWaitTimeout
	} else if cnf.Transaction.LockWaitTimeout > 0 && cnf.Transaction.LockWaitTimeout < time.Second {
		cnf.Transaction.LockWaitTimeout = cnf.Transaction.LockWaitTimeout * time.Second
	}
	if !cnf.Transaction.EnableCoalescing {
		cnf.Transaction.EnableCoalescing = defaultTransaction.EnableCoalescing
	}
	if cnf.Transaction.IndexQueuePrefix == "" {
		cnf.Transaction.IndexQueuePrefix = defaultTransaction.IndexQueuePrefix
	}
}

func (cnf *Configuration) setReconciliationDefaults() {
	if cnf.Reconciliation.DefaultStrategy == "" {
		cnf.Reconciliation.DefaultStrategy = defaultReconciliation.DefaultStrategy
	}
	if cnf.Reconciliation.ProgressInterval == 0 {
		cnf.Reconciliation.ProgressInterval = defaultReconciliation.ProgressInterval
	}
	if cnf.Reconciliation.MaxRetries == 0 {
		cnf.Reconciliation.MaxRetries = defaultReconciliation.MaxRetries
	}
	if cnf.Reconciliation.RetryDelay == 0 {
		cnf.Reconciliation.RetryDelay = defaultReconciliation.RetryDelay
	}
}

func (cnf *Configuration) setTypeSenseDefaults() {
	if cnf.TypeSense.BackpressureRetryInterval <= 0 {
		cnf.TypeSense.BackpressureRetryInterval = defaultTypeSense.BackpressureRetryInterval
	}
}

func (cnf *Configuration) setQueueDefaults() {
	if cnf.Queue.TransactionQueue == "" {
		cnf.Queue.TransactionQueue = defaultQueue.TransactionQueue
	}
	if cnf.Queue.WebhookQueue == "" {
		cnf.Queue.WebhookQueue = defaultQueue.WebhookQueue
	}
	if cnf.Queue.IndexQueue == "" {
		cnf.Queue.IndexQueue = defaultQueue.IndexQueue
	}
	if cnf.Queue.InflightExpiryQueue == "" {
		cnf.Queue.InflightExpiryQueue = defaultQueue.InflightExpiryQueue
	}
	if cnf.Queue.InflightCommitQueue == "" {
		cnf.Queue.InflightCommitQueue = defaultQueue.InflightCommitQueue
	}
	if cnf.Queue.NumberOfQueues == 0 {
		cnf.Queue.NumberOfQueues = defaultQueue.NumberOfQueues
	}
	if cnf.Queue.HotQueueName == "" {
		cnf.Queue.HotQueueName = defaultQueue.HotQueueName
	}
	if cnf.Queue.HotQueueConcurrency == 0 {
		cnf.Queue.HotQueueConcurrency = defaultQueue.HotQueueConcurrency
	}
	if cnf.Queue.HotPairTTL == 0 {
		cnf.Queue.HotPairTTL = defaultQueue.HotPairTTL
	} else {
		cnf.Queue.HotPairTTL = cnf.Queue.HotPairTTL * time.Second
	}
	if cnf.Queue.HotPairLockContentionThreshold == 0 {
		cnf.Queue.HotPairLockContentionThreshold = defaultQueue.HotPairLockContentionThreshold
	}
	if cnf.Queue.MaxRetryAttempts == 0 {
		cnf.Queue.MaxRetryAttempts = defaultQueue.MaxRetryAttempts
	}
	if cnf.Queue.MonitoringPort == "" {
		cnf.Queue.MonitoringPort = defaultQueue.MonitoringPort
	}
	if cnf.Queue.WebhookConcurrency == 0 {
		cnf.Queue.WebhookConcurrency = defaultQueue.WebhookConcurrency
	}
	if cnf.Queue.TransactionWorkerConcurrency == 0 {
		cnf.Queue.TransactionWorkerConcurrency = defaultQueue.TransactionWorkerConcurrency
	}
	if cnf.Queue.BackpressureMemoryPercent == 0 {
		cnf.Queue.BackpressureMemoryPercent = defaultQueue.BackpressureMemoryPercent
	}
	if cnf.Queue.BackpressureCheckInterval == 0 {
		cnf.Queue.BackpressureCheckInterval = defaultQueue.BackpressureCheckInterval
	}
}

func (cnf *Configuration) setRedisDefaults() {
	if cnf.Redis.PoolSize == 0 {
		cnf.Redis.PoolSize = defaultRedis.PoolSize
	}
	if cnf.Redis.MinIdleConns == 0 {
		cnf.Redis.MinIdleConns = defaultRedis.MinIdleConns
	}
	if cnf.Redis.ConnMaxIdleTime == 0 {
		cnf.Redis.ConnMaxIdleTime = defaultRedis.ConnMaxIdleTime
	}
}

func (cnf *Configuration) setDatabaseDefaults() {
	if cnf.DataSource.MaxOpenConns == 0 {
		cnf.DataSource.MaxOpenConns = defaultDatabase.MaxOpenConns
	}
	if cnf.DataSource.MaxIdleConns == 0 {
		cnf.DataSource.MaxIdleConns = defaultDatabase.MaxIdleConns
	}
	if cnf.DataSource.ConnMaxLifetime == 0 {
		cnf.DataSource.ConnMaxLifetime = defaultDatabase.ConnMaxLifetime
	}
	if cnf.DataSource.ConnMaxIdleTime == 0 {
		cnf.DataSource.ConnMaxIdleTime = defaultDatabase.ConnMaxIdleTime
	}
}

func (cnf *Configuration) trimWhitespace() {
	cnf.ProjectName = strings.TrimSpace(cnf.ProjectName)
	cnf.Server.Port = strings.TrimSpace(cnf.Server.Port)
	cnf.DataSource.Dns = strings.TrimSpace(cnf.DataSource.Dns)
	cnf.Redis.Dns = strings.TrimSpace(cnf.Redis.Dns)
}

// UploadDomainWhitelistHosts returns the parsed, trimmed, de-duplicated, lowercase
// list of exact hostnames permitted as targets for URL-based reconciliation
// uploads. Entries may be supplied as bare hostnames ("example.com") or full
// URLs ("https://example.com/path"); the hostname is extracted either way.
// An empty/unset whitelist yields an empty slice, which the upload handler
// treats as deny-by-default.
func (cnf *Configuration) UploadDomainWhitelistHosts() []string {
	raw := strings.TrimSpace(cnf.Server.UploadDomainWhitelist)
	if raw == "" {
		return nil
	}

	seen := make(map[string]struct{})
	hosts := make([]string, 0, 4)
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		// Tolerate entries supplied as full URLs by extracting the hostname.
		var host string
		if strings.Contains(entry, "://") {
			if u, err := url.Parse(entry); err == nil {
				host = u.Hostname()
			}
		} else {
			host = entry
		}
		host = strings.ToLower(strings.TrimSpace(host))
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	return hosts
}

func (cnf *Configuration) setupRateLimiting() {

	if cnf.RateLimit.RequestsPerSecond == nil && cnf.RateLimit.Burst == nil {
		defaultRPS := 5000000.0
		defaultBurst := 10000000

		cnf.RateLimit.RequestsPerSecond = &defaultRPS
		cnf.RateLimit.Burst = &defaultBurst

		logrus.WithFields(logrus.Fields{
			"rps":   defaultRPS,
			"burst": defaultBurst,
		}).Info("rate limiting not configured, using defaults")
	}

	if cnf.RateLimit.RequestsPerSecond != nil && cnf.RateLimit.Burst == nil {
		defaultBurst := 2 * int(*cnf.RateLimit.RequestsPerSecond)
		cnf.RateLimit.Burst = &defaultBurst
		logrus.WithField("burst", defaultBurst).Warn("rate limit burst not specified, setting default")
	}
	if cnf.RateLimit.RequestsPerSecond == nil && cnf.RateLimit.Burst != nil {
		defaultRPS := float64(*cnf.RateLimit.Burst) / 2
		cnf.RateLimit.RequestsPerSecond = &defaultRPS
		logrus.WithField("rps", defaultRPS).Warn("rate limit RPS not specified, setting default")
	}
	if cnf.RateLimit.CleanupIntervalSec == nil {
		defaultCleanup := DEFAULT_CLEANUP_SEC
		cnf.RateLimit.CleanupIntervalSec = &defaultCleanup
		logrus.WithField("cleanup_interval_sec", defaultCleanup).
			Warn("rate limit cleanup interval not specified, setting default")
	}
}

// MockConfig sets a mock configuration for testing purposes.
func MockConfig(mockConfig *Configuration) {
	err := mockConfig.validateAndAddDefaults()
	if err != nil {
		logrus.WithError(err).Error("error setting mock config")
		return
	}
	ConfigStore.Store(mockConfig)
}

func logger() {
	// Configure logrus defaults
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
}
