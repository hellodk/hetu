package config

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// EnvPrefix is prepended to every environment-variable override.
const EnvPrefix = "CI_"

// Diagnostics captures non-fatal config load issues. It is intended for UI
// surfacing and operator debugging when the process continues with defaults.
type Diagnostics struct {
	Warnings []string `json:"warnings"`
	Errors   []string `json:"errors"`
}

func (d *Diagnostics) warn(msg string) {
	d.Warnings = append(d.Warnings, msg)
}

func (d *Diagnostics) err(msg string) {
	d.Errors = append(d.Errors, msg)
}

// Load reads configuration from the given file path (if non-empty), then
// applies environment-variable overrides, then resolves any *File sibling
// fields by reading the referenced files. Finally it runs Validate.
//
// Loading is fail-fast: any error stops startup. The returned Config is
// safe to use directly; downstream packages should not re-validate.
func Load(path string) (Config, error) {
	cfg := Default()

	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("config: read %q: %w", path, err)
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("config: parse %q: %w", path, err)
		}
	}

	if err := applyEnv(&cfg); err != nil {
		return cfg, fmt.Errorf("config: env overrides: %w", err)
	}

	if err := resolveSecretFiles(&cfg); err != nil {
		return cfg, fmt.Errorf("config: resolve secret files: %w", err)
	}

	if err := Validate(&cfg); err != nil {
		return cfg, fmt.Errorf("config: validate: %w", err)
	}

	return cfg, nil
}

// LoadLayered reads an optional base config file and optional override config
// file, then applies env overrides and resolves *File secrets. Finally it
// validates the resulting config.
//
// Precedence (lowest → highest):
// - compiled defaults
// - basePath YAML
// - overridePath YAML
// - CI_* env vars
// - *File sibling fields (secrets)
func LoadLayered(basePath, overridePath string) (Config, error) {
	cfg := Default()

	if basePath != "" {
		raw, err := os.ReadFile(basePath)
		if err != nil {
			return cfg, fmt.Errorf("config: read %q: %w", basePath, err)
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("config: parse %q: %w", basePath, err)
		}
	}

	if overridePath != "" {
		raw, err := os.ReadFile(overridePath)
		if err != nil {
			return cfg, fmt.Errorf("config: read %q: %w", overridePath, err)
		}
		// Unmarshal into existing cfg so only provided keys override.
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("config: parse %q: %w", overridePath, err)
		}
	}

	if err := applyEnv(&cfg); err != nil {
		return cfg, fmt.Errorf("config: env overrides: %w", err)
	}
	if err := resolveSecretFiles(&cfg); err != nil {
		return cfg, fmt.Errorf("config: resolve secret files: %w", err)
	}
	if err := Validate(&cfg); err != nil {
		return cfg, fmt.Errorf("config: validate: %w", err)
	}
	return cfg, nil
}

// LoadLayeredRelaxed behaves like LoadLayered but never fails startup. It
// returns the best-effort config along with diagnostics describing what was
// wrong. Downstream systems should treat non-empty diagnostics.Errors as a
// degraded state and surface them to operators.
func LoadLayeredRelaxed(basePath, overridePath string) (Config, Diagnostics) {
	cfg := Default()
	var diag Diagnostics

	tryUnmarshal := func(path string) {
		if path == "" {
			return
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				diag.warn(fmt.Sprintf("config file %q not found; using defaults", path))
				return
			}
			diag.err(fmt.Sprintf("failed to read config file %q: %v", path, err))
			return
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			diag.err(fmt.Sprintf("failed to parse config file %q: %v", path, err))
			return
		}
	}

	tryUnmarshal(basePath)
	tryUnmarshal(overridePath)

	if err := applyEnv(&cfg); err != nil {
		diag.err(fmt.Sprintf("failed to apply env overrides: %v", err))
	}
	if err := resolveSecretFiles(&cfg); err != nil {
		diag.err(fmt.Sprintf("failed to resolve secret files: %v", err))
	}
	if err := Validate(&cfg); err != nil {
		diag.err(fmt.Sprintf("config validation failed: %v", err))
	}

	return cfg, diag
}

// LoadFromEnv is a convenience that resolves the file path from the
// CI_CONFIG environment variable and the supplied default. It returns the
// loaded Config or an error if loading fails. Use the empty string for
// defaultPath to require an explicit CI_CONFIG.
func LoadFromEnv(defaultPath string) (Config, error) {
	path := os.Getenv(EnvPrefix + "CONFIG")
	if path == "" {
		path = defaultPath
	}
	if path != "" {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			// Missing file is OK if defaults + env vars are sufficient;
			// Validate will catch real misconfiguration.
			path = ""
		}
	}
	return Load(path)
}

// ResolvePathsFromEnv returns the base config path and override config path
// according to environment variables. Empty strings mean “not set”.
func ResolvePathsFromEnv(defaultBasePath, defaultOverridePath string) (string, string) {
	base := os.Getenv(EnvPrefix + "CONFIG")
	if base == "" {
		base = defaultBasePath
	}
	override := os.Getenv(EnvPrefix + "CONFIG_OVERRIDE")
	if override == "" {
		override = defaultOverridePath
	}
	return base, override
}

// applyEnv walks the Config struct via reflection and overlays any matching
// environment variable. The mapping is: yaml path joined with "_", upper-cased,
// prefixed with EnvPrefix. Slices are comma-separated. Durations parse with
// time.ParseDuration. Booleans accept the usual forms.
func applyEnv(cfg *Config) error {
	return walkAndApplyEnv(reflect.ValueOf(cfg).Elem(), nil)
}

func walkAndApplyEnv(v reflect.Value, path []string) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		yamlTag := field.Tag.Get("yaml")
		if yamlTag == "" || yamlTag == "-" {
			continue
		}
		name := strings.SplitN(yamlTag, ",", 2)[0]
		if name == "" {
			continue
		}
		fieldPath := append(path, name)
		fv := v.Field(i)

		if fv.Kind() == reflect.Ptr {
			if fv.IsNil() {
				continue
			}
			fv = fv.Elem()
		}

		if fv.Kind() == reflect.Struct && fv.Type() != reflect.TypeOf(time.Time{}) {
			if err := walkAndApplyEnv(fv, fieldPath); err != nil {
				return err
			}
			continue
		}

		envKey := EnvPrefix + strings.ToUpper(strings.Join(envify(fieldPath), "_"))
		raw, ok := os.LookupEnv(envKey)
		if !ok {
			continue
		}
		if err := setScalar(fv, raw); err != nil {
			return fmt.Errorf("env %s: %w", envKey, err)
		}
	}
	return nil
}

// envify converts camelCase yaml field names to UPPER_SNAKE for env vars.
// Example: "passwordFile" -> "PASSWORD_FILE".
func envify(parts []string) []string {
	out := make([]string, len(parts))
	for i, p := range parts {
		var b strings.Builder
		for j, r := range p {
			if j > 0 && r >= 'A' && r <= 'Z' {
				b.WriteByte('_')
			}
			b.WriteRune(r)
		}
		out[i] = b.String()
	}
	return out
}

func setScalar(fv reflect.Value, raw string) error {
	if !fv.CanSet() {
		return errors.New("field is not settable")
	}
	switch fv.Kind() {
	case reflect.String:
		fv.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// time.Duration is an int64 with a TextUnmarshaler-style helper.
		if fv.Type() == reflect.TypeOf(time.Duration(0)) {
			d, err := time.ParseDuration(raw)
			if err != nil {
				return err
			}
			fv.SetInt(int64(d))
			return nil
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return err
		}
		fv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return err
		}
		fv.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return err
		}
		fv.SetFloat(f)
	case reflect.Slice:
		if fv.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("unsupported slice element type %s", fv.Type().Elem().Kind())
		}
		parts := strings.Split(raw, ",")
		out := reflect.MakeSlice(fv.Type(), len(parts), len(parts))
		for i, p := range parts {
			out.Index(i).SetString(strings.TrimSpace(p))
		}
		fv.Set(out)
	default:
		return fmt.Errorf("unsupported kind %s", fv.Kind())
	}
	return nil
}

// resolveSecretFiles walks every *File field; if it is set, it reads the
// file contents (trimmed of trailing whitespace) into the corresponding
// non-File field. The non-File field is overwritten unconditionally — file
// always wins, matching the §4.5 precedence rules.
func resolveSecretFiles(cfg *Config) error {
	pairs := []struct {
		filePath *string
		target   *string
		label    string
	}{
		{&cfg.Stores.Postgres.DSNFile, &cfg.Stores.Postgres.DSN, "stores.postgres.dsnFile"},
		{&cfg.Stores.Postgres.PasswordFile, &cfg.Stores.Postgres.Password, "stores.postgres.passwordFile"},
		{&cfg.Stores.ClickHouse.DSNFile, &cfg.Stores.ClickHouse.DSN, "stores.clickhouse.dsnFile"},
		{&cfg.Stores.ClickHouse.PasswordFile, &cfg.Stores.ClickHouse.Password, "stores.clickhouse.passwordFile"},
		{&cfg.Stores.Redis.AddrFile, &cfg.Stores.Redis.Addr, "stores.redis.addrFile"},
		{&cfg.Stores.Redis.PasswordFile, &cfg.Stores.Redis.Password, "stores.redis.passwordFile"},
		{&cfg.Bus.NATS.URLFile, &cfg.Bus.NATS.URL, "bus.nats.urlFile"},
		{&cfg.Bus.NATS.PasswordFile, &cfg.Bus.NATS.Password, "bus.nats.passwordFile"},
		{&cfg.Bus.NATS.TokenFile, &cfg.Bus.NATS.Token, "bus.nats.tokenFile"},
		{&cfg.LLM.APIKeyFile, &cfg.LLM.APIKey, "llm.apiKeyFile"},
	}
	for _, p := range pairs {
		if *p.filePath == "" {
			continue
		}
		raw, err := os.ReadFile(*p.filePath)
		if err != nil {
			return fmt.Errorf("%s: read %q: %w", p.label, *p.filePath, err)
		}
		*p.target = strings.TrimRight(string(raw), " \t\r\n")
	}
	return nil
}

// Validate enforces structural rules. Called automatically by Load; exposed
// so tests and binaries can re-validate after manual mutations.
func Validate(cfg *Config) error {
	if cfg.Cluster.ID == "" {
		return errors.New("cluster.id is required")
	}

	if cfg.Stores.Postgres.Enabled {
		pg := cfg.Stores.Postgres
		if pg.DSN == "" {
			if pg.Host == "" || pg.Database == "" || pg.User == "" {
				return errors.New("stores.postgres: host, database, user required when DSN unset")
			}
			if pg.Port == 0 {
				return errors.New("stores.postgres.port is required")
			}
		}
	}

	if cfg.Stores.ClickHouse.Enabled {
		ch := cfg.Stores.ClickHouse
		if ch.DSN == "" {
			if len(ch.Hosts) == 0 || ch.Database == "" {
				return errors.New("stores.clickhouse: hosts and database required when DSN unset")
			}
			if ch.Port == 0 {
				return errors.New("stores.clickhouse.port is required")
			}
		}
	}

	if cfg.Stores.Redis.Enabled {
		rd := cfg.Stores.Redis
		if rd.Addr == "" && rd.Sentinel == nil {
			return errors.New("stores.redis: addr or sentinel required")
		}
		if rd.Sentinel != nil {
			if rd.Sentinel.MasterName == "" || len(rd.Sentinel.Addrs) == 0 {
				return errors.New("stores.redis.sentinel: masterName and addrs required")
			}
		}
	}

	if cfg.Bus.NATS.Enabled && !cfg.Bus.NATS.Embedded && cfg.Bus.NATS.URL == "" {
		return errors.New("bus.nats: url required when not embedded")
	}

	if cfg.LLM.Provider != "" {
		switch cfg.LLM.Provider {
		case "openai", "anthropic", "azure", "ollama", "vllm", "bedrock", "llamacpp", "custom":
		default:
			return fmt.Errorf("llm.provider %q is not recognised", cfg.LLM.Provider)
		}
		// Local/self-hosted providers don't need API keys
		// We allow APIKey to be empty and surface the issue at first call time
		// so misconfigured non-LLM workloads still boot.
	}

	return nil
}
