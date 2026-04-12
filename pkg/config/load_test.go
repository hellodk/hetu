package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load empty path: %v", err)
	}
	if cfg.Cluster.ID != "default" {
		t.Errorf("expected default cluster id, got %q", cfg.Cluster.ID)
	}
	if cfg.Stores.Postgres.Enabled {
		t.Errorf("postgres should be disabled by default")
	}
	if cfg.LLM.Provider != "ollama" {
		t.Errorf("default llm provider should be ollama, got %q", cfg.LLM.Provider)
	}
}

func TestLoadFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := `
cluster:
  id: test-cluster
  displayName: Test
stores:
  postgres:
    enabled: true
    host: db.example.com
    port: 5432
    database: ci
    user: ci
    password: hunter2
    sslMode: require
    connMaxLifetime: 15m
bus:
  nats:
    enabled: true
    url: nats://nats.example.com:4222
llm:
  provider: anthropic
  endpoint: https://api.anthropic.com
  model: claude-sonnet-4-6
  apiKey: secret
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Cluster.ID != "test-cluster" {
		t.Errorf("cluster.id = %q", cfg.Cluster.ID)
	}
	if !cfg.Stores.Postgres.Enabled {
		t.Errorf("postgres should be enabled")
	}
	if cfg.Stores.Postgres.Host != "db.example.com" {
		t.Errorf("postgres host = %q", cfg.Stores.Postgres.Host)
	}
	if cfg.Stores.Postgres.ConnMaxLifetime != 15*time.Minute {
		t.Errorf("postgres connMaxLifetime = %v", cfg.Stores.Postgres.ConnMaxLifetime)
	}
	if cfg.LLM.Provider != "anthropic" {
		t.Errorf("llm provider = %q", cfg.LLM.Provider)
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("CI_CLUSTER_ID", "from-env")
	t.Setenv("CI_STORES_POSTGRES_ENABLED", "true")
	t.Setenv("CI_STORES_POSTGRES_HOST", "envhost")
	t.Setenv("CI_STORES_POSTGRES_PORT", "6543")
	t.Setenv("CI_STORES_POSTGRES_DATABASE", "envdb")
	t.Setenv("CI_STORES_POSTGRES_USER", "envuser")
	t.Setenv("CI_STORES_POSTGRES_CONN_MAX_LIFETIME", "1h")
	t.Setenv("CI_STORES_CLICKHOUSE_HOSTS", "ch1,ch2,ch3")
	t.Setenv("CI_STORES_CLICKHOUSE_ENABLED", "true")
	t.Setenv("CI_STORES_CLICKHOUSE_DATABASE", "ci")
	t.Setenv("CI_STORES_CLICKHOUSE_PORT", "9000")
	t.Setenv("CI_KUBE_QPS", "75")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Cluster.ID != "from-env" {
		t.Errorf("cluster.id = %q", cfg.Cluster.ID)
	}
	if !cfg.Stores.Postgres.Enabled {
		t.Errorf("postgres.enabled not honoured from env")
	}
	if cfg.Stores.Postgres.Host != "envhost" {
		t.Errorf("postgres.host = %q", cfg.Stores.Postgres.Host)
	}
	if cfg.Stores.Postgres.Port != 6543 {
		t.Errorf("postgres.port = %d", cfg.Stores.Postgres.Port)
	}
	if cfg.Stores.Postgres.ConnMaxLifetime != time.Hour {
		t.Errorf("postgres.connMaxLifetime = %v", cfg.Stores.Postgres.ConnMaxLifetime)
	}
	if got := cfg.Stores.ClickHouse.Hosts; len(got) != 3 || got[0] != "ch1" || got[2] != "ch3" {
		t.Errorf("clickhouse.hosts = %v", got)
	}
	if cfg.Kube.QPS != 75 {
		t.Errorf("kube.qps = %v", cfg.Kube.QPS)
	}
}

func TestSecretFileResolution(t *testing.T) {
	dir := t.TempDir()
	pwPath := filepath.Join(dir, "pgpass")
	if err := os.WriteFile(pwPath, []byte("very-secret\n"), 0600); err != nil {
		t.Fatalf("write pgpass: %v", err)
	}
	apikeyPath := filepath.Join(dir, "llmkey")
	if err := os.WriteFile(apikeyPath, []byte("sk-test\n"), 0600); err != nil {
		t.Fatalf("write apikey: %v", err)
	}

	t.Setenv("CI_STORES_POSTGRES_ENABLED", "true")
	t.Setenv("CI_STORES_POSTGRES_HOST", "h")
	t.Setenv("CI_STORES_POSTGRES_PORT", "5432")
	t.Setenv("CI_STORES_POSTGRES_DATABASE", "d")
	t.Setenv("CI_STORES_POSTGRES_USER", "u")
	t.Setenv("CI_STORES_POSTGRES_PASSWORD", "should-be-replaced")
	t.Setenv("CI_STORES_POSTGRES_PASSWORD_FILE", pwPath)
	t.Setenv("CI_LLM_API_KEY_FILE", apikeyPath)

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Stores.Postgres.Password != "very-secret" {
		t.Errorf("password from file = %q", cfg.Stores.Postgres.Password)
	}
	if cfg.LLM.APIKey != "sk-test" {
		t.Errorf("llm api key from file = %q", cfg.LLM.APIKey)
	}
}

func TestValidatePostgresMissingFields(t *testing.T) {
	cfg := Default()
	cfg.Stores.Postgres.Enabled = true
	cfg.Stores.Postgres.Host = ""
	if err := Validate(&cfg); err == nil {
		t.Errorf("expected validation error for postgres without host")
	}
}

func TestValidateUnknownLLMProvider(t *testing.T) {
	cfg := Default()
	cfg.LLM.Provider = "not-a-thing"
	if err := Validate(&cfg); err == nil {
		t.Errorf("expected validation error for unknown llm provider")
	}
}
