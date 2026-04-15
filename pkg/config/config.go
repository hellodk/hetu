// Package config defines the unified configuration model for every binary
// in the cluster-intel project. See docs/PLAN_V7.md §4.5 for the design.
//
// Loading precedence (lowest to highest):
//
//  1. Compiled-in defaults (Default()).
//  2. YAML file at the path supplied to Load.
//  3. Environment variables with the CI_ prefix mirroring the YAML structure
//     (e.g. CI_STORES_POSTGRES_HOST).
//  4. *File sibling fields — for any sensitive value, the file contents
//     replace the inline value at startup. Trimmed of trailing whitespace.
//
// CLI flags are handled by individual binaries; this package is flag-agnostic.
package config

import "time"

// Config is the root configuration object.
type Config struct {
	Cluster ClusterConfig `yaml:"cluster"`
	Server  ServerConfig  `yaml:"server"`
	Analyzer AnalyzerConfig `yaml:"analyzer"`
	Kube    KubeConfig    `yaml:"kube"`
	Stores  StoresConfig  `yaml:"stores"`
	Bus     BusConfig     `yaml:"bus"`
	LLM     LLMConfig     `yaml:"llm"`
	Logging LoggingConfig `yaml:"logging"`
}

// ClusterConfig identifies the cluster this instance reports on.
type ClusterConfig struct {
	ID          string `yaml:"id"`
	DisplayName string `yaml:"displayName"`
}

// ServerConfig controls the HTTP servers exposed by a binary.
type ServerConfig struct {
	APIPort     int    `yaml:"apiPort"`
	MetricsPort int    `yaml:"metricsPort"`
	BindAddress string `yaml:"bindAddress"`
}

// AnalyzerConfig captures analyzer-specific runtime config that isn't shared
// across all binaries.
type AnalyzerConfig struct {
	CollectorURL        string        `yaml:"collectorUrl"`
	PrometheusURL       string        `yaml:"prometheusUrl"`
	CORSAllowedOrigins  []string      `yaml:"corsAllowedOrigins"`
	AnalysisInterval    time.Duration `yaml:"analysisInterval"`
	ScanSecurityInterval time.Duration `yaml:"scanSecurityInterval"`
	ScanPodHealthInterval time.Duration `yaml:"scanPodHealthInterval"`
	ScanAnomalyInterval time.Duration `yaml:"scanAnomalyInterval"`
	ScanOptimizerInterval time.Duration `yaml:"scanOptimizerInterval"`
}

// KubeConfig controls how a binary connects to the Kubernetes API.
type KubeConfig struct {
	InCluster      bool    `yaml:"inCluster"`
	KubeconfigPath string  `yaml:"kubeconfigPath"`
	QPS            float32 `yaml:"qps"`
	Burst          int     `yaml:"burst"`
}

// StoresConfig groups every persistent store the project may talk to.
// Each entry has its own Enabled flag so binaries can opt in only to what
// they need.
type StoresConfig struct {
	Postgres   PostgresConfig   `yaml:"postgres"`
	ClickHouse ClickHouseConfig `yaml:"clickhouse"`
	Redis      RedisConfig      `yaml:"redis"`
}

// PostgresConfig describes a Postgres endpoint.
//
// Either DSN (or DSNFile) may be set to short-circuit the individual fields.
// PasswordFile, if set, wins over Password at load time.
type PostgresConfig struct {
	Enabled         bool          `yaml:"enabled"`
	DSN             string        `yaml:"dsn"`
	DSNFile         string        `yaml:"dsnFile"`
	Host            string        `yaml:"host"`
	Port            int           `yaml:"port"`
	Database        string        `yaml:"database"`
	User            string        `yaml:"user"`
	Password        string        `yaml:"password"`
	PasswordFile    string        `yaml:"passwordFile"`
	SSLMode         string        `yaml:"sslMode"`
	SSLRootCertFile string        `yaml:"sslRootCertFile"`
	MaxOpenConns    int           `yaml:"maxOpenConns"`
	MaxIdleConns    int           `yaml:"maxIdleConns"`
	ConnMaxLifetime time.Duration `yaml:"connMaxLifetime"`
	MigrationsPath  string        `yaml:"migrationsPath"`
	AppName         string        `yaml:"appName"`
}

// ClickHouseConfig describes a ClickHouse endpoint or cluster.
type ClickHouseConfig struct {
	Enabled        bool          `yaml:"enabled"`
	DSN            string        `yaml:"dsn"`
	DSNFile        string        `yaml:"dsnFile"`
	Hosts          []string      `yaml:"hosts"`
	Port           int           `yaml:"port"`
	Database       string        `yaml:"database"`
	User           string        `yaml:"user"`
	Password       string        `yaml:"password"`
	PasswordFile   string        `yaml:"passwordFile"`
	Secure         bool          `yaml:"secure"`
	DialTimeout    time.Duration `yaml:"dialTimeout"`
	MaxOpenConns   int           `yaml:"maxOpenConns"`
	MigrationsPath string        `yaml:"migrationsPath"`
}

// RedisConfig describes a Redis endpoint or Sentinel topology.
type RedisConfig struct {
	Enabled      bool                 `yaml:"enabled"`
	Addr         string               `yaml:"addr"`
	AddrFile     string               `yaml:"addrFile"`
	Username     string               `yaml:"username"`
	Password     string               `yaml:"password"`
	PasswordFile string               `yaml:"passwordFile"`
	DB           int                  `yaml:"db"`
	TLS          bool                 `yaml:"tls"`
	DialTimeout  time.Duration        `yaml:"dialTimeout"`
	PoolSize     int                  `yaml:"poolSize"`
	Sentinel     *RedisSentinelConfig `yaml:"sentinel,omitempty"`
}

// RedisSentinelConfig optionally enables Redis Sentinel discovery.
type RedisSentinelConfig struct {
	MasterName string   `yaml:"masterName"`
	Addrs      []string `yaml:"addrs"`
}

// BusConfig groups every async bus implementation.
type BusConfig struct {
	NATS NATSConfig `yaml:"nats"`
}

// NATSConfig describes a NATS endpoint with optional embedded mode for dev.
type NATSConfig struct {
	Enabled       bool   `yaml:"enabled"`
	URL           string `yaml:"url"`
	URLFile       string `yaml:"urlFile"`
	Token         string `yaml:"token"`
	TokenFile     string `yaml:"tokenFile"`
	User          string `yaml:"user"`
	Password      string `yaml:"password"`
	PasswordFile  string `yaml:"passwordFile"`
	NkeyFile      string `yaml:"nkeyFile"`
	CredsFile     string `yaml:"credsFile"`
	TLS           bool   `yaml:"tls"`
	TLSCAFile     string `yaml:"tlsCaFile"`
	StreamPrefix  string `yaml:"streamPrefix"`
	Embedded      bool   `yaml:"embedded"`
	EmbeddedStore string `yaml:"embeddedStore"`
}

// LLMConfig describes the LLM provider used by the analyzer.
type LLMConfig struct {
	Provider             string        `yaml:"provider"`
	Endpoint             string        `yaml:"endpoint"`
	Model                string        `yaml:"model"`
	APIKey               string        `yaml:"apiKey"`
	APIKeyFile           string        `yaml:"apiKeyFile"`
	MaxTokens            int           `yaml:"maxTokens"`
	Temperature          float64       `yaml:"temperature"`
	Timeout              time.Duration `yaml:"timeout"`
	MaxRetries           int           `yaml:"maxRetries"`
	DailyTokenBudget     int           `yaml:"dailyTokenBudget"`
	ExplainOptimizations bool          `yaml:"explainOptimizations"`
}

// LoggingConfig controls structured logging.
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Default returns a Config populated with sensible local-dev defaults.
// Production deployments override these via YAML + env vars.
func Default() Config {
	return Config{
		Cluster: ClusterConfig{
			ID:          "default",
			DisplayName: "default",
		},
		Server: ServerConfig{
			APIPort:     8080,
			MetricsPort: 9090,
			BindAddress: "0.0.0.0",
		},
		Analyzer: AnalyzerConfig{
			CollectorURL:         "",
			PrometheusURL:        "",
			CORSAllowedOrigins:   []string{"*"},
			AnalysisInterval:     5 * time.Minute,
			ScanSecurityInterval: 5 * time.Minute,
			ScanPodHealthInterval: 2 * time.Minute,
			ScanAnomalyInterval:  3 * time.Minute,
			ScanOptimizerInterval: 10 * time.Minute,
		},
		Kube: KubeConfig{
			InCluster: true,
			QPS:       50,
			Burst:     100,
		},
		Stores: StoresConfig{
			Postgres: PostgresConfig{
				Enabled:         false,
				Host:            "localhost",
				Port:            5432,
				Database:        "cluster_intel",
				User:            "cluster_intel",
				SSLMode:         "disable",
				MaxOpenConns:    25,
				MaxIdleConns:    5,
				ConnMaxLifetime: 30 * time.Minute,
				MigrationsPath:  "/etc/cluster-intel/migrations/postgres",
				AppName:         "cluster-intel",
			},
			ClickHouse: ClickHouseConfig{
				Enabled:        false,
				Hosts:          []string{"localhost"},
				Port:           9000,
				Database:       "cluster_intel",
				User:           "default",
				DialTimeout:    10 * time.Second,
				MaxOpenConns:   10,
				MigrationsPath: "/etc/cluster-intel/migrations/clickhouse",
			},
			Redis: RedisConfig{
				Enabled:     false,
				Addr:        "localhost:6379",
				DialTimeout: 5 * time.Second,
				PoolSize:    20,
			},
		},
		Bus: BusConfig{
			NATS: NATSConfig{
				Enabled:      false,
				URL:          "nats://localhost:4222",
				StreamPrefix: "ci",
			},
		},
		LLM: LLMConfig{
			Provider:         "ollama",
			Endpoint:         "http://localhost:11434",
			Model:            "llama3",
			MaxTokens:        2048,
			Temperature:      0.2,
			Timeout:          90 * time.Second,
			MaxRetries:       3,
			DailyTokenBudget: 1_000_000,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
	}
}
