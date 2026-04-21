# Repo Hygiene & Standardisation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove tracked artifacts that should never be in git, fix a CI script that references deleted services, reorganise misplaced files, and ensure .gitignore prevents the same mistakes from re-occurring.

**Architecture:** Eight independent, bite-sized tasks in ascending risk order — pure git-index cleanup first, scripts/ci surgery last. Each task ends with a commit so the history stays bisectable.

**Tech Stack:** bash, git, Go, scripts/ci (project shell CLI)

---

## Breaking-Change Assessment

| Task | Risk | If skipped |
|------|------|-----------|
| 1 – untrack .playwright-mcp | None | 18 YAML blobs bloat every clone |
| 2 – untrack docs/ui-mockups PNGs | None | 17 PNG blobs bloat every clone |
| 3 – .gitignore additions | None | workspace file re-tracked on next commit |
| 4 – fix scripts/ci | **HIGH** | `ci build go`, `ci test unit/lint`, `ci deploy stack`, `ci logs`, `ci doctor` all try to operate on `src/collector-podlogs` / `src/collector-lblogs` which no longer exist — loops iterate missing dirs (bash exits on `-e`), and the `ci deploy podlogs/lblogs` sub-commands apply a Docker image that will never be built again |
| 5 – move HTML mockups | Low | root-level clutter; no code references them |
| 6 – delete legacy e2e/ tests | Low | 7 orphaned JS scripts never run by any CI step |
| 7 – remove src/api/ dir | None | empty directory; untracked; cosmetic |
| 8 – CHANGELOG | None | stale docs |

---

## File Map

| File | Change |
|------|--------|
| `.gitignore` | Add `*.code-workspace` and `tests/node_modules/` |
| `.playwright-mcp/*.yml` (18) | `git rm --cached` |
| `docs/ui-mockups/**/*.png` (17) | `git rm --cached` |
| `k8s-cluster-health.code-workspace` | `git rm --cached` (keep local copy) |
| `scripts/ci` | Remove 4 stale service references, 2 entire stale functions, 2 stale dispatch cases |
| `mockup-option-a.html` → `docs/ui-mockups/option-a.html` | `git mv` |
| `mockup-option-b.html` → `docs/ui-mockups/option-b.html` | `git mv` |
| `src/e2e/*.js` (7 files) | Delete — superseded by `src/dashboard/tests/` |
| `package.json` (root) | Delete — only served the now-deleted e2e scripts |
| `package-lock.json` (root) | Delete |
| `src/api/` | `rmdir` — empty local dir, nothing tracked |
| `CHANGELOG.md` | Add v7.0.0 section |

---

## Task 1: Untrack .playwright-mcp/ snapshot files

Claude Code's browser automation writes page snapshots to `.playwright-mcp/`. These were committed before the `.gitignore` rule was added. The rule now exists; we just need to remove the files from the git index.

**Files:**
- Modify (git index only): `.playwright-mcp/*.yml` (18 files)

- [ ] **Step 1: Confirm the files are tracked**

```bash
git ls-files '.playwright-mcp/'
```

Expected: 18 lines of `page-YYYY-MM-DDTHH-MM-SS-mmmZ.yml`

- [ ] **Step 2: Remove from index without deleting local copies**

```bash
git rm --cached .playwright-mcp/*.yml
```

Expected: `rm '.playwright-mcp/page-2026-04-15T19-52-11-069Z.yml'` (×18)

- [ ] **Step 3: Verify .gitignore already covers the pattern**

```bash
git check-ignore -v .playwright-mcp/page-2026-04-15T19-52-11-069Z.yml
```

Expected: `.gitignore:NN:.playwright-mcp/`

- [ ] **Step 4: Confirm the files won't be re-staged**

```bash
git status --short | grep playwright
```

Expected: no output (files are ignored, not untracked)

- [ ] **Step 5: Commit**

```bash
git add .gitignore
git commit -m "chore: untrack .playwright-mcp snapshot files from git index

These Claude Code browser-automation artifacts were committed before the
.gitignore rule existed. Rule is already present; this removes the 18
tracked YAML files without deleting the local copies."
```

---

## Task 2: Untrack docs/ui-mockups/ PNG screenshots

17 PNG screenshots under `docs/ui-mockups/*/screenshots/` and `docs/ui-mockups/themes-chooser.png` are tracked despite the `.gitignore` rule `docs/ui-mockups/**/*.png`. They were committed before the rule existed.

**Files:**
- Modify (git index only): `docs/ui-mockups/**/*.png` (17 files)

- [ ] **Step 1: Confirm the files are tracked**

```bash
git ls-files 'docs/ui-mockups/' | grep '\.png$'
```

Expected: 17 paths (4 themes × 4 screenshots + themes-chooser.png)

- [ ] **Step 2: Remove from index**

```bash
git rm --cached \
  'docs/ui-mockups/aurora/screenshots/mockup-00-design-system.png' \
  'docs/ui-mockups/aurora/screenshots/mockup-01-overview.png' \
  'docs/ui-mockups/aurora/screenshots/mockup-02-executive.png' \
  'docs/ui-mockups/aurora/screenshots/mockup-03-incident-detail.png' \
  'docs/ui-mockups/calm-signal/screenshots/mockup-00-design-system.png' \
  'docs/ui-mockups/calm-signal/screenshots/mockup-01-overview.png' \
  'docs/ui-mockups/calm-signal/screenshots/mockup-02-executive.png' \
  'docs/ui-mockups/calm-signal/screenshots/mockup-03-incident-detail.png' \
  'docs/ui-mockups/graphite/screenshots/mockup-00-design-system.png' \
  'docs/ui-mockups/graphite/screenshots/mockup-01-overview.png' \
  'docs/ui-mockups/graphite/screenshots/mockup-02-executive.png' \
  'docs/ui-mockups/graphite/screenshots/mockup-03-incident-detail.png' \
  'docs/ui-mockups/prism/screenshots/mockup-00-design-system.png' \
  'docs/ui-mockups/prism/screenshots/mockup-01-overview.png' \
  'docs/ui-mockups/prism/screenshots/mockup-02-executive.png' \
  'docs/ui-mockups/prism/screenshots/mockup-03-incident-detail.png' \
  'docs/ui-mockups/themes-chooser.png'
```

Expected: `rm 'docs/ui-mockups/...'` (×17)

- [ ] **Step 3: Verify ignored**

```bash
git check-ignore -v docs/ui-mockups/aurora/screenshots/mockup-01-overview.png
```

Expected: `.gitignore:NN:docs/ui-mockups/**/*.png`

- [ ] **Step 4: Commit**

```bash
git commit -m "chore: untrack ui-mockup PNG screenshots from git index

PNGs were captured before the gitignore rule existed.
Rule docs/ui-mockups/**/*.png already present — remove the 17
blob entries. Local files are preserved."
```

---

## Task 3: Add missing .gitignore entries

Two categories are not yet covered: IDE workspace files and the `tests/` node_modules tree (which Task 6 will create).

**Files:**
- Modify: `.gitignore`

- [ ] **Step 1: Read the existing IDE / personal block**

```bash
grep -n 'workspace\|\.code-workspace\|\.idea\|\.vscode' .gitignore
```

Expected: no hits (the pattern is not yet present)

- [ ] **Step 2: Add the new rules**

Append to the "Personal scratch / session notes" or IDE block in `.gitignore`:

```
# ---- IDE workspace files --------------------------------------------------
*.code-workspace

# ---- tests/ harness -------------------------------------------------------
tests/node_modules/
```

Exact edit — find the block that ends with `.champ/` and add after it:

```
# ---- IDE workspace files --------------------------------------------------
*.code-workspace

# ---- tests/ legacy e2e (node_modules) ------------------------------------
tests/node_modules/
```

- [ ] **Step 3: Untrack the workspace file**

```bash
git rm --cached k8s-cluster-health.code-workspace
```

Expected: `rm 'k8s-cluster-health.code-workspace'`

- [ ] **Step 4: Verify workspace file is now ignored**

```bash
git check-ignore -v k8s-cluster-health.code-workspace
```

Expected: `.gitignore:NN:*.code-workspace`

- [ ] **Step 5: Commit**

```bash
git add .gitignore
git commit -m "chore: add *.code-workspace and tests/node_modules/ to .gitignore

Workspace files are IDE-personal; tests/node_modules/ will be written
by Task 6 if e2e harness is revived under tests/."
```

---

## Task 4: Fix scripts/ci — remove stale collector-podlogs/collector-lblogs references

**This is the highest-risk task.** After the collector consolidation (commit `514c815`), `src/collector-podlogs/` and `src/collector-lblogs/` were deleted. Eight places in `scripts/ci` (849 lines) still reference them. Any engineer or CI pipeline running these commands today will hit errors.

**Files:**
- Modify: `scripts/ci`

### 4a — SERVICES array (line 87)

- [ ] **Step 1: Edit SERVICES array**

Old:
```bash
SERVICES=(collector analyzer collector-podlogs collector-lblogs)
```

New:
```bash
SERVICES=(collector analyzer)
```

### 4b — Remove dispatch cases for deleted sub-commands (lines 190–191)

- [ ] **Step 2: Remove stale deploy dispatch cases**

Old block inside `cmd_deploy()` case statement:
```bash
        podlogs)  cmd_deploy_podlogs "$@" ;;
        lblogs)   cmd_deploy_lblogs "$@" ;;
```

New (delete both lines entirely — the case falls through to the `*) error` branch if someone types `ci deploy podlogs`):
```bash
```
_(lines deleted)_

### 4c — Delete cmd_deploy_podlogs function (lines 332–373)

- [ ] **Step 3: Delete entire cmd_deploy_podlogs function**

The function spans from `cmd_deploy_podlogs() {` through its closing `}`. It emits a `collector-podlogs` Deployment YAML that references the image `${REGISTRY}/cluster-intel-collector-podlogs:${tag}` which will never be built again.

Delete from:
```bash
cmd_deploy_podlogs() {
    header "Deploy Pod Log Collector"
```

Through the closing `}` and trailing blank line (approximately 42 lines).

### 4d — Delete cmd_deploy_lblogs function (lines 375–453)

- [ ] **Step 4: Delete entire cmd_deploy_lblogs function**

Same rationale — references `cluster-intel-collector-lblogs` image that no longer exists. Delete from `cmd_deploy_lblogs() {` through closing `}` (~79 lines including the interactive LB config builder).

### 4e — cmd_test_unit loop (line 470)

- [ ] **Step 5: Remove stale dirs from unit-test loop**

Old:
```bash
    for d in pkg/config pkg/store pkg/bus pkg/kube pkg/llm pkg/types pkg/middleware \
             src/collector src/analyzer src/collector-podlogs src/collector-lblogs; do
```

New:
```bash
    for d in pkg/config pkg/store pkg/bus pkg/kube pkg/llm pkg/types pkg/middleware \
             src/collector src/analyzer; do
```

### 4f — cmd_test_lint loop (line 494)

- [ ] **Step 6: Remove stale dirs from lint loop**

Old:
```bash
    for d in src/collector src/analyzer src/collector-podlogs src/collector-lblogs; do
```

New:
```bash
    for d in src/collector src/analyzer; do
```

### 4g — cmd_logs select menu (line 590)

- [ ] **Step 7: Remove stale choices from logs selector**

Old:
```bash
    _prompt_select "Which component?" \
        "collector" "analyzer" "dashboard" "collector-podlogs" "collector-lblogs" "all"
```

New:
```bash
    _prompt_select "Which component?" \
        "collector" "analyzer" "dashboard" "all"
```

### 4h — cmd_doctor tidy loop (line 650)

- [ ] **Step 8: Remove stale dirs from doctor/tidy loop**

Old:
```bash
    for d in pkg/config pkg/store pkg/bus pkg/kube pkg/llm src/collector src/analyzer \
             src/collector-podlogs src/collector-lblogs; do
```

New:
```bash
    for d in pkg/config pkg/store pkg/bus pkg/kube pkg/llm src/collector src/analyzer; do
```

### 4i — Smoke-test the edited script

- [ ] **Step 9: Run bash syntax check**

```bash
bash -n scripts/ci
```

Expected: no output (syntax OK)

- [ ] **Step 10: Grep for any remaining stale strings**

```bash
grep -n 'collector-podlogs\|collector-lblogs' scripts/ci
```

Expected: no output

- [ ] **Step 11: Commit**

```bash
git add scripts/ci
git commit -m "fix(ci): remove stale collector-podlogs/collector-lblogs references

Both services were consolidated into the unified collector binary in
commit 514c815. Remove the SERVICES array entries, two entire deploy
functions, two dispatch cases, and four service-list occurrences in
test/lint/doctor loops."
```

---

## Task 5: Move root HTML mockups into docs/ui-mockups/

`mockup-option-a.html` and `mockup-option-b.html` were committed to the repo root. They belong under `docs/ui-mockups/` with the other design artefacts.

**Files:**
- Rename: `mockup-option-a.html` → `docs/ui-mockups/option-a.html`
- Rename: `mockup-option-b.html` → `docs/ui-mockups/option-b.html`

- [ ] **Step 1: Verify no code references the root paths**

```bash
grep -r 'mockup-option' . --include='*.go' --include='*.ts' --include='*.tsx' --include='*.sh' --include='*.md' | grep -v '.git/'
```

Expected: no output (or only this plan document itself)

- [ ] **Step 2: git mv both files**

```bash
git mv mockup-option-a.html docs/ui-mockups/option-a.html
git mv mockup-option-b.html docs/ui-mockups/option-b.html
```

- [ ] **Step 3: Confirm rename was detected**

```bash
git status
```

Expected:
```
Changes to be committed:
  renamed:    mockup-option-a.html -> docs/ui-mockups/option-a.html
  renamed:    mockup-option-b.html -> docs/ui-mockups/option-b.html
```

- [ ] **Step 4: Commit**

```bash
git commit -m "chore: move root HTML mockups into docs/ui-mockups/

option-a and option-b prototypes belong alongside the other design
artefacts rather than in the repo root."
```

---

## Task 6: Delete obsolete src/e2e/ tests and root package.json

`src/e2e/` contains 7 one-off JavaScript test scripts (dashboard.test.js, test-comprehensive.js, test-dashboard.js, test-final.js, test-loading-state.js, test-responsive-hover.js, test-ui.js) that predate the proper Playwright suite now living at `src/dashboard/tests/`. They are not run by any active CI step — `Makefile:test-e2e` points to `src/dashboard` and `scripts/ci cmd_test_e2e` also targets the dashboard suite.

The root `package.json` and `package-lock.json` exist solely to provide `playwright` for these scripts. Neither is referenced by any build or CI target.

**Files:**
- Delete: `src/e2e/*.js` (7 files)
- Delete: `package.json` (root)
- Delete: `package-lock.json` (root)

- [ ] **Step 1: Confirm no Makefile/CI target points at src/e2e/**

```bash
grep -rn 'src/e2e' Makefile scripts/ .github/ 2>/dev/null
```

Expected: no output

- [ ] **Step 2: Confirm the active test target does NOT use root package.json**

```bash
grep -n 'test-e2e\|test:e2e' Makefile
```

Expected:
```
26:test-e2e:
28:	@cd src/dashboard && npm run test:e2e
```
(points to src/dashboard, not root)

- [ ] **Step 3: Remove the files**

```bash
git rm src/e2e/dashboard.test.js \
       src/e2e/test-comprehensive.js \
       src/e2e/test-dashboard.js \
       src/e2e/test-final.js \
       src/e2e/test-loading-state.js \
       src/e2e/test-responsive-hover.js \
       src/e2e/test-ui.js \
       package.json \
       package-lock.json
```

- [ ] **Step 4: Confirm directory is gone from index**

```bash
git ls-files 'src/e2e/'
```

Expected: no output

- [ ] **Step 5: Remove the now-empty directory locally**

```bash
rmdir src/e2e/
```

- [ ] **Step 6: Commit**

```bash
git commit -m "chore: delete obsolete src/e2e/ scripts and root package.json

Seven ad-hoc JS test scripts from before the Playwright suite (src/dashboard/tests/)
was established. No CI step runs them. Root package.json existed solely to
provide playwright for these scripts."
```

---

## Task 7: Remove empty src/api/ directory

`src/api/` is an empty local directory with no tracked files. It appears to be a placeholder from early project scaffolding.

**Files:**
- Delete (local only): `src/api/`

- [ ] **Step 1: Confirm nothing is tracked there**

```bash
git ls-files 'src/api/'
```

Expected: no output

- [ ] **Step 2: Confirm directory is empty**

```bash
ls -la src/api/
```

Expected: only `.` and `..` entries

- [ ] **Step 3: Remove directory**

```bash
rmdir src/api/
```

- [ ] **Step 4: Verify no breakage**

```bash
grep -rn 'src/api' Makefile scripts/ go.work 2>/dev/null
```

Expected: no output

_No commit needed — nothing tracked changed._

---

## Task 8: Update CHANGELOG.md with v7.0.0

The last CHANGELOG entry is `[6.0.0]`. The collector consolidation (commit `514c815`) is a significant architectural change that warrants a new major-version entry.

**Files:**
- Modify: `CHANGELOG.md`

- [ ] **Step 1: Read the existing [6.0.0] header to match the format**

```bash
head -40 CHANGELOG.md
```

- [ ] **Step 2: Insert v7.0.0 section immediately after `# Changelog` heading**

```markdown
## [7.0.0] — 2026-04-20

### Breaking Changes
- `src/collector-podlogs/` and `src/collector-lblogs/` deleted. Both subsystems
  are now integrated into the unified `src/collector/` binary.
- The Docker images `cluster-intel-collector-podlogs` and `cluster-intel-collector-lblogs`
  are no longer built. Use `cluster-intel-collector` with the new env vars below.
- `scripts/ci deploy podlogs` and `scripts/ci deploy lblogs` sub-commands removed.

### Added
- Unified collector binary with Go build-tag–controlled subsystems:
  - `ENABLE_PODLOGS=true` / `WATCH_NAMESPACES` — pod log streaming (always compiled)
  - `ENABLE_LBLOGS=true` — LB access-log ingestion (excluded via `nolblogs` build tag)
- `make docker-build-lean` target — builds collector without AWS SDK (`-tags nolblogs`)
- Helm `values.yaml` gains `collector.podlogs.*` and `collector.lblogs.*` blocks
- `docker-compose.yml` gains `ENABLE_PODLOGS`, `WATCH_NAMESPACES`, `ENABLE_LBLOGS`,
  `LB_CONFIGS`, `CW_LOG_GROUPS`, `AWS_REGION`, `DELIVERY_MODE` env vars

### Removed
- `src/collector-podlogs/` — merged into `src/collector/podlogs*.go`
- `src/collector-lblogs/` — merged into `src/collector/lblogs*.go` (build-tag gated)
- `go.work` entries for the deleted modules

---
```

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: add CHANGELOG entry for v7.0.0 collector consolidation"
```

---

## Self-Review

**Spec coverage check:**

| Finding from audit | Task covering it |
|--------------------|-----------------|
| 18 tracked .playwright-mcp YAMLs | Task 1 |
| 17 tracked docs/ui-mockups PNGs | Task 2 |
| k8s-cluster-health.code-workspace tracked | Task 3 |
| Missing tests/node_modules/ in .gitignore | Task 3 |
| scripts/ci 8 stale references | Task 4 |
| Root HTML mockups misplaced | Task 5 |
| src/e2e/ 7 orphaned JS tests | Task 6 |
| Root package.json/lock orphaned | Task 6 |
| src/api/ empty directory | Task 7 |
| CHANGELOG missing v7.0.0 | Task 8 |

**Placeholder check:** All steps contain exact commands and expected output. No TBDs.

**Type consistency:** No code types involved across tasks.

**Gaps found:** None — all 10 audit findings are covered.
