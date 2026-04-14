# `scripts/run-local.sh` — usage

Operator-facing CLI for everything local: configure → validate → start
the analyzer + dashboard. Modeled on the `avk` pattern (interactive
prompts with validated defaults). Replaces the old hand-edit-the-env-file
flow so a typo or port collision is caught **before** anything starts.

---

## TL;DR

```bash
# Interactive (recommended first time)
scripts/run-local.sh

# Accept env-file defaults silently — fastest way to start
scripts/run-local.sh start --yes

# Run against UAT env file
scripts/run-local.sh start --yes -e uat

# Stop / status / logs
scripts/run-local.sh stop
scripts/run-local.sh status
scripts/run-local.sh logs analyzer

# Pre-flight without starting anything
scripts/run-local.sh doctor

# Wizard to write a fresh env/dev.env
scripts/run-local.sh setup -e dev
```

`make` shortcuts:

```bash
make doctor              # = scripts/run-local.sh doctor
make run    ENV=dev      # = scripts/run-local.sh start --yes -e dev
make stop                # = scripts/run-local.sh stop
make status              # = scripts/run-local.sh status
```

---

## Sub-commands

| Command | What it does |
|---|---|
| `start` | Load env file → prompt for every config item → validate → start analyzer + dashboard → `wait_for_http` until both respond |
| `stop` | Kill via pidfile, clean up `/tmp/cluster-intel-logs/*.pid` |
| `status` | Tabular: `SERVICE / PIDFILE / PID / HTTP / URL` |
| `restart` | `stop` then `start` |
| `logs [name]` | `tail -F` `analyzer`, `dashboard`, `collector`, or `all` (default) |
| `build` | Rebuild the analyzer binary if the source is newer |
| `setup` | Interactive wizard → write `env/<env>.env` (asks before overwriting) |
| `doctor` | Pre-flight: tools, env-file, kubeconfig, port availability, binary freshness. **Never mutates state.** |
| `lint` | Validate the env file's syntax + values without touching services |
| `version` | Print `cluster-intel: git <sha> · chart <ver>` |
| `help` | Usage |

Run with no command for the interactive picker:

```
scripts/run-local.sh

  What would you like to do? [default: start]
    1)* start
    2)  stop
    3)  status
    4)  restart
    ...
```

---

## Global flags

| Flag | Description |
|---|---|
| `-e ENV`, `--env ENV` | Pick `env/<ENV>.env`. Defaults to `$ENVIRONMENT` or `dev`. |
| `-y`, `--yes` | Accept all env-file defaults silently. No prompts. |
| `--non-interactive` | Hard-fail if any required value is missing. Use in CI. |
| `--env-file PATH` | Override the env-file path entirely (ignores `-e`). |
| `--log-dir PATH` | Override `/tmp/cluster-intel-logs`. |

Notes:
- `--yes` and `--non-interactive` are independent. Combine them in CI:
  ```bash
  scripts/run-local.sh start --yes --non-interactive -e prod
  ```
- When `/dev/tty` isn't openable (cron jobs, subprocesses, CI containers)
  the script **auto-promotes to `--yes`** so it can't crash on a missing
  terminal.

---

## Env files

Loaded from `env/<ENVIRONMENT>.env`. Provided in-repo:

| File | Tracked | Contents |
|---|---|---|
| `env/dev.env` | yes | Local-laptop / LAN-demo defaults (PROFILE=live or mock, Ollama localhost, etc.) |
| `env/uat.env` | yes | Staging-like (live profile, real LLM provider) |
| `env/prod.env.example` | yes | Template — copy to `env/prod.env` (gitignored) |

The script source-loads with `set -a` so children inherit. **CLI-level
env vars override file-level**, e.g.:

```bash
LLM_PROVIDER=openai LLM_MODEL=gpt-4 scripts/run-local.sh start --yes
```

Override the file path entirely:

```bash
ENV_FILE=/path/to/custom.env scripts/run-local.sh start --yes
```

---

## Configurable variables

The script prompts for each in this order. The env-file value (or
built-in default) appears in `[brackets]`; press enter to accept.

### Runtime

| Variable | Default | Validator |
|---|---|---|
| `ENVIRONMENT` | `dev` | `dev|uat|prod` |
| `PROFILE` | `mock` | `live|mock` — drives whether Collector / LLM API key prompts fire |
| `BIND_ADDRESS` | `0.0.0.0` | non-empty (analyzer + collector listen interface) |
| `NEXT_HOSTNAME` | `0.0.0.0` | next dev / next start hostname |
| `DASHBOARD_MODE` | `dev` | `dev|prod` (`prod` triggers `npm run build` first) |

### Ports

All validated as integer 1–65535 **and** not currently bound:

| Variable | Default |
|---|---|
| `ANALYZER_PORT` | `18081` |
| `METRICS_PORT` | `19091` |
| `DASHBOARD_PORT` | `3003` |
| `COLLECTOR_PORT` | `18080` |
| `COLLECTOR_METRICS_PORT` | `19090` |

### Collector (only when `PROFILE=live`)

| Variable | Default | Validator |
|---|---|---|
| `COLLECTOR_URL` | empty | URL syntax (hard-fail) + reachability (warn-only) |

If empty, the analyzer reports "collector unreachable" until the
operator points it somewhere via the dashboard's Settings modal or
`POST /api/v1/collector`.

### LLM

| Variable | Default | Validator |
|---|---|---|
| `LLM_PROVIDER` | `ollama` | one of `ollama|openai|anthropic|vllm|llamacpp|azure|bedrock|none` |
| `LLM_ENDPOINT` | `http://localhost:11434` | URL syntax + reachability (warn-only); skipped when `provider=none` |
| `LLM_MODEL` | `llama3` | non-empty |
| `LLM_API_KEY` | empty | masked input; not required for `ollama|llamacpp|none` |

### Intervals (Go duration syntax: `30s`, `5m`, `1h30m`)

| Variable | Default |
|---|---|
| `ANALYSIS_INTERVAL` | `30s` |
| `EVICT_INTERVAL` | `30s` |
| `MOCK_INTERVAL` | `20s` |

### Kubernetes (only when `PROFILE=live`)

| Variable | Default | Validator |
|---|---|---|
| `KUBECONFIG` | `~/.kube/config` | file exists + `kubectl --kubeconfig=$KUBECONFIG cluster-info` succeeds within 5 s |

---

## Validation policy

| Severity | What it catches |
|---|---|
| **Hard-fail** (exits non-zero) | Port already bound (names the conflicting PID), missing kubeconfig in live mode, bad enum, bad integer, missing required value in `--non-interactive`, port < 1024 or > 65535, bad Go duration |
| **Soft-warn** (continues) | URL not reachable (network may be transient), empty `COLLECTOR_URL` in live mode |

Validators re-prompt on failure. With `--non-interactive`, a re-prompt
becomes a `fatal`.

---

## Common recipes

### First-time setup

```bash
scripts/run-local.sh doctor              # confirm tools are present
scripts/run-local.sh setup -e dev        # write env/dev.env
scripts/run-local.sh start               # interactive — verify each prompt
```

### Daily dev loop

```bash
make run ENV=dev                         # uses --yes silently
make status
make stop
```

### Switching between environments

```bash
scripts/run-local.sh start --yes -e dev
# … later …
scripts/run-local.sh stop
scripts/run-local.sh start --yes -e uat
```

### Running a one-off command against UAT without overwriting env files

```bash
LLM_PROVIDER=openai LLM_MODEL=gpt-4 LLM_API_KEY=sk-… \
  scripts/run-local.sh start --yes -e uat
```

### CI invocation (no terminal at all)

```bash
scripts/run-local.sh start --yes --non-interactive -e prod
```

If anything is missing, the script exits non-zero with a clear message
naming the offending variable.

### Investigate failed startup

```bash
scripts/run-local.sh status         # which service is missing?
scripts/run-local.sh logs analyzer  # tail just one
scripts/run-local.sh logs           # tail all (default)
```

### Pre-flight without starting

```bash
scripts/run-local.sh doctor
# checks bash, go, npx, curl, ss, kubectl, helm versions
# checks env file presence + syntax
# checks kubeconfig reachability (live mode only)
# checks all 4 ports are free
# reports analyzer-binary freshness
```

### Validate just the env file

```bash
scripts/run-local.sh lint -e prod
```

---

## Behavior when there's no terminal

Scenario: cron, CI, claude background task, `bash -c …`.

`/dev/tty` is not openable → the script **auto-promotes to `--yes`** at
load time. Every prompt returns its default. If a default is missing
under `--non-interactive`, the script aborts cleanly:

```
[error] non-interactive: required value 'COLLECTOR_URL' missing
```

This means you can pipe to the script without it crashing on `set -u`:

```bash
echo "" | scripts/run-local.sh start -e dev   # works; uses every default
```

---

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Validation failure, missing tool, port collision, kubeconfig invalid, unknown command, user aborted |

Status / logs / version always return 0 (informational commands don't
fail on missing services — they just print "stale" / "-").

---

## Logs and pidfiles

Default location: `/tmp/cluster-intel-logs/` (override with `--log-dir`).

```
/tmp/cluster-intel-logs/
├── analyzer.log
├── analyzer.pid
├── dashboard.log
├── dashboard.pid
└── collector.{log,pid}    # if a collector was started
```

`stop` removes the pidfiles. `status` reports `(stale)` for pidfiles
whose PID is no longer running.

---

## What it does NOT do

- Persist edits back into the env file (use `setup` to write a fresh one).
- Start the collector. The analyzer talks to a separate collector
  process; if you want the local stack with collector, start it yourself
  and point `COLLECTOR_URL` at it.
- Generate systemd units. This is for interactive dev; production goes
  through the Helm chart at `deploy/helm/cluster-intel/`.
- Cross-host orchestration. Single-machine only.
- TLS bootstrap. HTTP only locally.

For production deployments, see [`deploy/helm/cluster-intel/README.md`](../deploy/helm/cluster-intel/README.md)
and [`docs/MIGRATION.md`](MIGRATION.md).

---

## Reference: prompt helpers (for script extenders)

If you fork the script, three helpers do all the prompting:

```bash
_prompt_input  "Label" "default"             # → PROMPT_RESULT
_prompt_secret "Label" "default"             # masked, → PROMPT_RESULT
_prompt_select "Title" "default" "opt1" …    # menu, → PROMPT_RESULT
prompt_confirm "Question?" [Y|N]             # returns 0 (yes) or 1 (no)
```

Result lands in the global `PROMPT_RESULT` to avoid the
`var=$(prompt_func)` subshell-capture quirk that strips colour and
breaks `read`. All four respect `YES` (silent default) and
`NON_INTERACTIVE` (fatal on missing default).

Validators follow the convention `validate_<thing> "VAR_NAME" "value" [args…]`,
return 0 on pass, non-zero on fail, and print `[error]` on fail.

---

## Troubleshooting

### `port collision — change ANALYZER_PORT or stop the listener`

Something is already on the port. The error names the PID:

```
[error] ANALYZER_PORT :18081 is already bound — listener: LISTEN 0 4096 *:18081 *:* users:(("analyzer",pid=2371151,fd=9))
```

Either `scripts/run-local.sh stop` (if it's our previous instance) or
`kill <pid>`.

### `kubectl cluster-info failed using KUBECONFIG=…`

In live mode the script verifies the kubeconfig is usable. Either:
- Switch profile to `mock` for a no-K8s demo, or
- Fix `KUBECONFIG` (`export KUBECONFIG=/path/to/config`)

### `Dashboard failed to become reachable`

Check the dashboard log:
```bash
tail -50 /tmp/cluster-intel-logs/dashboard.log
```
Common cause: `npm` not installed, or `next.config.js` syntax error.

### Service starts but data is empty

`PROFILE=live` requires a running collector. Either:
- Start a collector and set `COLLECTOR_URL`, or
- Switch to `PROFILE=mock` (you'll see synthetic data with a "MOCK"
  watermark).

### Want to keep prompts but skip just one or two

Pre-set the env var on the CLI, then run interactively:

```bash
LLM_API_KEY=sk-… scripts/run-local.sh start
```

The wizard will still prompt for `LLM_PROVIDER`, `LLM_MODEL`, etc., but
the API key won't ask again (it's already set in the environment).

---

## Files touched by this script

| Path | Purpose |
|---|---|
| `bin/analyzer` | Output of `cmd_build`; rebuilt on `start` if source is newer |
| `/tmp/cluster-intel-logs/{analyzer,dashboard,collector}.log` | Stdout/stderr capture |
| `/tmp/cluster-intel-logs/{analyzer,dashboard,collector}.pid` | PID tracking |
| `env/<env>.env` | Read on every command; written by `setup` |

Nothing else is mutated.
