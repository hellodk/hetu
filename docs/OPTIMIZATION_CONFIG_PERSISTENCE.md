# Optimization Review: UI Config Persistence & Graceful Startup

This document reviews the implementation of **UI-driven configuration + persistence** and calls out correctness gaps, operational risks, and high-value optimizations.

## Scope reviewed
- Unified config loader (`pkg/config/*`)
- Analyzer runtime persistence and config APIs (`src/analyzer/*`)
- Dashboard Settings UX (`src/dashboard/*`)
- Helm wiring + RBAC (`deploy/helm/cluster-intel/*`)
- Relevant design intent in `docs/PLAN_V7.md` §4.5

## What is implemented (matches intent)

### Layered config resolution + graceful boot
- **Layering** is implemented via:
  - Base: `CI_CONFIG` (default `/etc/cluster-intel/config.yaml`)
  - Override: `CI_CONFIG_OVERRIDE` (default `/etc/cluster-intel/runtime.yaml`)
  - Env: `CI_*`
- **Relaxed load mode** exists (`LoadLayeredRelaxed`) that returns diagnostics instead of failing startup.
- Analyzer surfaces diagnostics via `GET /api/v1/status` (`config.errors`, `config.warnings`).

### Persistence backends
- A `ConfigStore` abstraction exists:
  - **In-cluster**: `cluster-intel-runtime` ConfigMap key `runtime.yaml`
  - **Local**: filesystem runtime override YAML

### Config APIs
- `GET/PUT /api/v1/config` supports:
  - Reading effective config + diagnostics + persisted runtime YAML
  - Writing a runtime override YAML layer (and persisting it)
- Collector URL changes persist to runtime overrides from the UI (`/api/v1/collector`).
- LLM config changes persist non-secret fields to runtime overrides (API keys are not persisted).

### UI behavior when config is missing/incorrect
- The dashboard renders a diagnostic panel when live dependencies are down.
- A **10-minute reminder** is emitted while Live mode is blocked by missing/unreachable Collector (or LLM is down).

### Helm / RBAC
- Runtime override ConfigMap is mounted and `CI_CONFIG_OVERRIDE` is set.
- Namespaced RBAC is added for the runtime override ConfigMap mutations.

## Correctness notes / gaps

### 1) Runtime override mounted read-only is fine (but don’t expect file writes)
The analyzer persists overrides via the K8s API (ConfigMap updates), not by writing `/etc/cluster-intel/runtime.yaml` in-place. That’s correct (ConfigMap mounts are effectively read-only).

**Optimization**: In docs/UI text, be explicit that persistence is **ConfigMap-backed** in Kubernetes, not “writing the mounted file”.

### 2) RBAC: avoid `create` on ConfigMaps
Kubernetes RBAC cannot reliably restrict `create` to a single `resourceName`. The chart renders `cluster-intel-runtime` by default, so `create` is unnecessary.

**Status**: Updated RBAC to only allow `get/update/patch` for `cluster-intel-runtime`.

### 3) Partial “apply without restart” semantics
`PUT /api/v1/config` persists the override YAML, but only a **subset** is applied immediately in-memory (collector URL + some LLM runtime fields).

**Optimization options**:
- **A (recommended)**: Make it explicit in API response: `appliedNow: [keys...]`, `requiresRestart: [keys...]`.
- **B**: Add a lightweight “reload effective config” path that rebinds:
  - CORS middleware config
  - scan intervals/tickers (restart tickers)
  - Prometheus URL consumers (optimizers/anomaly detectors)

### 4) Unified config coverage vs legacy env usage
The analyzer still uses several legacy env vars (`PROMETHEUS_URL`, etc.) and now also has `ucfg.Analyzer.*`.

**Optimization**:
- Consolidate: always prefer unified config, keep legacy env vars as compatibility only, and surface in `/api/v1/config` which layer provided a value (optional “source map”).

### 5) YAML override shape is “power user” UX
The Settings YAML editor is powerful, but not “forms for major groups”.

**Optimization**:
- Add basic forms for the top 5 operator knobs:
  - Collector URL
  - CORS origins
  - Analysis interval + scan intervals
  - Prometheus URL
  - LLM provider/endpoint/model/maxTokens/temp (already exists)
- Keep YAML editor as “Advanced”.

### 6) Dashboard nags via toasts only
Toasts are easy to miss.

**Optimization**:
- Add a sticky banner (dismissible per-session) when live is blocked.
- Include a single CTA: “Open Settings” and scroll to the broken field.

## Documentation drift found (needs follow-up)

### `README.md` vs actual repo state
The repository README contains content that does not match the current Go/Next architecture in several places (e.g., references to Python/SQLite/old endpoints). It’s not part of the implementation plan, but it will confuse operators.

**Optimization**:
- Create a short “Current architecture & config” section:
  - `CI_CONFIG` + `CI_CONFIG_OVERRIDE` layering
  - Runtime overrides via ConfigMap `cluster-intel-runtime`
  - UI Settings for Collector + LLM + runtime overrides

### `docs/script_usage.md` claims interactive prompting not present
The doc describes a fully interactive wizard-like `scripts/run-local.sh`; the current script in the repo is simpler and non-interactive.

**Optimization**:
- Either implement the described wizard, or correct the documentation to match reality.

## Security hardening opportunities
- Add authn/authz before allowing config edits (even simple shared secret for now) if exposed beyond localhost.
- Add server-side validation for override YAML:
  - forbid secrets (`llm.apiKey`, `*password*`, etc.)
  - validate URL formats and durations
- Rate-limit config mutation endpoints separately.
- Audit log for config changes (who/what/when/source IP).

## Suggested next optimizations (high ROI)
1. **Typed config forms** for key settings + keep YAML “Advanced”.
2. **Config reload semantics**: apply more changes without restart, or clearly communicate restart required.
3. **Validation + redaction** pipeline for overrides.
4. **Sticky banner** for misconfiguration instead of only toasts.
5. **Docs alignment** (`README.md`, `deploy/helm/cluster-intel/README.md`) with the new config layers and runtime override model.

