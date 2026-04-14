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
| `start` | Load env file → prompt for every config item → validate → start analyzer + dashboard → `wait_for_http` until both respond. In `DASHBOARD_MODE=prod` runs `npm run build` synchronously first so build failures abort `start` cleanly. |
| `stop` | Kill via pidfile + tear down the whole process group (`setsid` leader + `pgrep -P` tree walk) so re-parented children like `next-server` don't survive. SIGTERM, then SIGKILL after 1s. Cleans `/tmp/cluster-intel-logs/*.pid`. |
| `status` | Tabular: `SERVICE / PIDFILE / PID / HTTP / URL` |
| `restart` | `stop` then `start` — relies on `stop` truly releasing the ports |
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

### Precedence

```
CLI / parent env  >  env-file value  >  built-in default
```

The script snapshots every relevant variable **before** sourcing the
env file and restores those values **after**, so what you set on the
command line always wins:

```bash
PROFILE=mock scripts/run-local.sh start --yes -e dev
# → uses PROFILE=mock even though env/dev.env has PROFILE=live

LLM_PROVIDER=openai LLM_MODEL=gpt-4 LLM_API_KEY=sk-… \
  scripts/run-local.sh start --yes -e uat
# → uses CLI-supplied LLM trio, ignores env-file LLM_*
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

### Collector

| Variable | Default | Validator |
|---|---|---|
| `COLLECTOR_URL` | `http://localhost:$COLLECTOR_PORT` (i.e. the local collector) | scheme auto-prepended if missing → URL syntax (hard-fail) → reachability (warn-only) |

Always prompted (even in `PROFILE=mock`) so the env file always carries
a value the operator can swap to live mode without re-editing. Required
when `PROFILE=live`; if empty in live, the analyzer reports
"collector unreachable" until the operator sets one via the dashboard's
Settings modal or `POST /api/v1/collector`.

### URL auto-scheme

For both `COLLECTOR_URL` and `LLM_ENDPOINT`, if you type a value without
a scheme the script prepends `http://` automatically:

```
COLLECTOR_URL [http://collector:8080]: 192.168.1.10:8080
[info]    COLLECTOR_URL → http://192.168.1.10:8080 (auto-prepended http://)
```

`https://` URLs are left untouched.

### LLM

| Variable | Default | Validator |
|---|---|---|
| `LLM_PROVIDER` | `ollama` | one of `ollama|openai|anthropic|vllm|llamacpp|azure|bedrock|none` |
| `LLM_ENDPOINT` | `http://localhost:11434` | scheme auto-prepended → URL syntax → reachability (warn-only); skipped when `provider=none` |
| `LLM_MODEL` | `llama3` | non-empty; **discovered from the endpoint** when interactive — see below |
| `LLM_API_KEY` | empty | masked input; not required for `ollama|llamacpp|none` |

### Smart-router model discovery

After the operator picks `LLM_PROVIDER` + `LLM_ENDPOINT`, the script
queries the endpoint to enumerate the models actually available there:

| Provider | Endpoint hit | Field extracted |
|---|---|---|
| `ollama` | `GET <endpoint>/api/tags` | `.models[].name` |
| `openai`, `anthropic`, `vllm`, `llamacpp`, `azure`, `bedrock` | `GET <endpoint>/v1/models` (with `Authorization: Bearer $LLM_API_KEY` if set) | `.data[].id` |

If the endpoint returns models, the operator sees a numbered picker
with the current `LLM_MODEL` as the default:

```
LLM_MODEL [default: qwen2.5:7b-instruct]
  1)* qwen2.5:7b-instruct
  2)  Qwen2.5-Coder:14B-Instruct
  3)  Qwen2.5-Coder:7b-instruct
  4)  llama3.1:8b
  5)  nomic-embed-text:latest
  ...
  Select [1-9, enter for default]:
```

If discovery fails (network down, unreachable endpoint, unsupported
provider, missing/wrong API key) the script transparently falls back to
free-text input — no surprise failure modes.

`--yes` / `--non-interactive` paths skip the picker and use whatever
`LLM_MODEL` the env file specified, so CI runs are deterministic.

### Intervals

Bare integers are accepted and normalised to **seconds** (the natural
unit for these short intervals). Suffix-bearing values (`5m`, `1h30m`,
`90s`) pass through unchanged.

| Variable | Default | Bare-integer assumption |
|---|---|---|
| `ANALYSIS_INTERVAL` | `30s` | seconds |
| `EVICT_INTERVAL` | `30s` | seconds |
| `MOCK_INTERVAL` | `20s` | seconds |

```
ANALYSIS_INTERVAL [30s]: 45
[info]    ANALYSIS_INTERVAL → 45s (assumed s)
```

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

### How `stop` finds and kills child processes

`npx next dev` forks a chain (`npx → node → next-server`). Once the
immediate child exits, `next-server` gets re-parented to init and would
survive a naive `kill <wrapper-pid>`. The script handles this two ways:

1. **Process group**: dashboard is launched via `setsid` so it becomes
   its own process-group leader. `stop` sends `kill -TERM -- -<pgid>`
   which signals the entire group, catching `next-server` regardless of
   re-parenting.
2. **Tree walk**: belt-and-braces, `kill_tree` recurses through
   `pgrep -P <pid>` and signals every descendant before the leader.
3. **Escalation**: if anything is still alive 1 second after `SIGTERM`,
   `SIGKILL` follows.

The end state is verifiable: after `stop`, `ss -tlnp | grep -E ':3003|:18081'`
returns nothing.

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

## Sub-command verification matrix

Last verified end-to-end at commit `54649fb2`. Each sub-command was run
against a clean state on a 3-node K8s cluster (`cylon`, `raspberrypi`,
`typhoon`):

| Command | What was checked | Result |
|---|---|---|
| `help` | usage prints | ✓ |
| `version` | git SHA + chart version | ✓ |
| `status` (clean) | table shows `-` for everything | ✓ |
| `status` (running) | table shows real PIDs + `200` | ✓ |
| `doctor` | flags port collisions when services running | ✓ |
| `lint -e dev` | passes; warns on empty `COLLECTOR_URL` | ✓ |
| `lint -e uat` | passes silently | ✓ |
| `build` | rebuilds analyzer | ✓ |
| `start --yes -e dev` | both services reach `200` from `127.0.0.1` and `192.168.1.10` | ✓ |
| `stop` | **all ports released** (verified via `ss -tlnp`) | ✓ |
| `restart` | works because stop is correct | ✓ |
| `logs` | `tail -F` all three log files | ✓ |
| `nopesuch` | clean `Unknown command:` error + usage | ✓ |
| `--non-interactive` | fatals cleanly when required value missing | ✓ |
| `PROFILE=mock ./run-local.sh start --yes` | **CLI overrides env-file** (was `profile=live` before fix) | ✓ |
| `LLM_ENDPOINT=192.168.1.10:11434 ./run-local.sh start --yes` | **scheme auto-prepended** to `http://192.168.1.10:11434` | ✓ |
| `discover_models ollama …` (interactive) | enumerates 9 real Ollama models, presents as picker | ✓ |
| `discover_models ollama http://10.0.0.99:…` | unreachable endpoint → empty output → falls back to free-text | ✓ |
| `COLLECTOR_URL=my-collector:8080 ./run-local.sh start --yes` (mock) | always prompts/normalises COLLECTOR_URL even in mock profile | ✓ |
| `ANALYSIS_INTERVAL=30 ./run-local.sh start --yes` | bare int normalised to `30s` (was hard-fail "must be Go duration") | ✓ |

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
