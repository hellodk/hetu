# Deployment guide

Covers Docker image builds, Docker Hub publishing, local compose stack, and
Helm-based production deployment.

---

## TL;DR

```bash
# Build all images and push to Docker Hub
make docker-build VERSION=7.0.0
make docker-push  VERSION=7.0.0

# Validate the Helm chart
helm lint deploy/helm/cluster-intel/
helm template cluster-intel deploy/helm/cluster-intel/ --namespace cluster-intel

# Deploy to Kubernetes (dependencies already in charts/)
make helm-deploy NAMESPACE=cluster-intel

# Local compose stack (Qdrant + collector + analyzer + dashboard)
docker compose up -d
```

---

## Prerequisites

| Tool | Minimum version | Purpose |
|------|----------------|---------|
| Docker | 24+ | Build + push images |
| Helm | 3.14+ | Chart linting, templating, deploy |
| kubectl | 1.28+ | Cluster access for `helm-deploy` |
| make | any | Makefile targets |

Check everything at once:

```bash
docker version
helm version --short
kubectl version --client
```

---

## 1. Docker images

### Image registry

All images are published to Docker Hub under `hellodk/`:

| Image | Tag | Size |
|-------|-----|------|
| `hellodk/cluster-intel-collector` | `7.0.0`, `latest` | ~40 MB |
| `hellodk/cluster-intel-analyzer` | `7.0.0`, `latest` | ~41 MB |
| `hellodk/cluster-intel-dashboard` | `7.0.0`, `latest` | ~247 MB |

Go images use `FROM scratch` at runtime — no OS layer, CA certs only.
Dashboard uses `node:20-alpine` with Next.js standalone output.

> **Note:** `collector-podlogs` and `collector-lblogs` were consolidated into
> the single `collector` binary in v7.0.0. Use build tags to control which
> subsystems are compiled in (`nolblogs` to exclude the AWS SDK).

### Build

Build all three images from the repo root:

```bash
make docker-build                        # uses REGISTRY=hellodk VERSION=7.0.0
make docker-build VERSION=8.0.0          # override version tag
make docker-build REGISTRY=myorg         # override registry prefix
```

What the target does:
1. Builds `collector` and `analyzer` using each service's `Dockerfile` with
   **build context = repo root** (needed so `COPY pkg/ ./pkg/` can reach
   shared packages).
2. Builds `dashboard` with **build context = `src/dashboard/`** (no shared Go
   packages; Next.js standalone output copied from builder stage).
3. Tags each image as both `:VERSION` and `:latest` in one pass.

> **Why repo root as context for Go services?**
> Each Go service imports from `pkg/` (shared modules). The Dockerfile copies
> `pkg/` and the service source into the builder stage. Docker build context
> must be the repo root so both directories are accessible. The dashboard is
> fully self-contained so its context is just `src/dashboard/`.

### Push

```bash
make docker-push                         # push both :VERSION and :latest for all 3 images
make docker-push VERSION=8.0.0 REGISTRY=myorg
```

Requires `docker login` first:

```bash
docker login                             # prompts for Docker Hub credentials
docker login -u hellodk                  # specify username
```

### Pull pre-built images

```bash
docker pull hellodk/cluster-intel-collector:7.0.0
docker pull hellodk/cluster-intel-analyzer:7.0.0
docker pull hellodk/cluster-intel-dashboard:7.0.0
```

### Build a single image

```bash
# collector only
docker build -t hellodk/cluster-intel-collector:7.0.0 \
  -f src/collector/Dockerfile .

# dashboard only
docker build -t hellodk/cluster-intel-dashboard:7.0.0 \
  -f src/dashboard/Dockerfile src/dashboard/
```

---

## 2. Local development stack (`docker compose`)

`docker-compose.yml` at the repo root runs the full stack locally:

```
qdrant        ← vector store (port 6333)
collector     ← K8s API watcher (port 18080)
analyzer      ← scoring + AI engine (port 18081)
dashboard     ← Next.js UI (port 3003)
```

```bash
# Start everything
docker compose up -d

# Start specific services only
docker compose up -d qdrant analyzer dashboard

# Watch logs
docker compose logs -f analyzer
docker compose logs -f dashboard

# Rebuild after a code change
docker compose build analyzer
docker compose up -d --no-deps analyzer    # restart just analyzer

# Stop and remove containers (volumes preserved)
docker compose down

# Full wipe including volumes
docker compose down -v
```

For local development without Docker, use `run-local.sh` instead — see
[`docs/script_usage.md`](script_usage.md).

---

## 3. Helm chart

The chart lives at `deploy/helm/cluster-intel/` and bundles PostgreSQL,
Redis, and NATS as optional sub-charts (enabled by default).

### Chart metadata

| Field | Value |
|-------|-------|
| Chart name | `cluster-intel` |
| Chart version | `0.2.0` |
| App version | `7.0.0` |
| Bundled deps | postgresql `15.5.38`, redis `19.6.4`, nats `1.3.16` |

### Validate (lint + template)

Always lint before deploying:

```bash
# Lint — checks template syntax, required fields, schema
helm lint deploy/helm/cluster-intel/

# Dry-render all manifests to stdout
helm template cluster-intel deploy/helm/cluster-intel/ \
  --namespace cluster-intel

# Dry-render with a custom values override
helm template cluster-intel deploy/helm/cluster-intel/ \
  --namespace cluster-intel \
  -f values-prod.yaml.example

# Server-side dry-run against a live cluster
helm install cluster-intel deploy/helm/cluster-intel/ \
  --namespace cluster-intel --create-namespace \
  --dry-run --debug
```

### Install / upgrade

```bash
# One-liner — installs or upgrades in place
make helm-deploy NAMESPACE=cluster-intel

# Equivalent manual command
helm upgrade --install cluster-intel deploy/helm/cluster-intel/ \
  --namespace cluster-intel --create-namespace \
  --wait --timeout 5m

# With a values override file
helm upgrade --install cluster-intel deploy/helm/cluster-intel/ \
  --namespace cluster-intel --create-namespace \
  -f values-prod.yaml.example \
  --wait --timeout 5m

# Override image tags at deploy time
helm upgrade --install cluster-intel deploy/helm/cluster-intel/ \
  --namespace cluster-intel --create-namespace \
  --set collector.image.tag=7.0.0 \
  --set analyzer.image.tag=7.0.0 \
  --set dashboard.image.tag=7.0.0
```

### Values files

| File | Purpose |
|------|---------|
| `deploy/helm/cluster-intel/values.yaml` | Upstream defaults (Docker Hub images, bundled deps) |
| `values-dev.yaml` | Local / dev overrides |
| `values-uat.yaml` | UAT overrides |
| `values-prod.yaml.example` | Template — copy to `values-prod.yaml` (gitignored) |
| `values-deploy.yaml` | Per-cluster production override passed via `-f` |

Override convention:

```
values.yaml  <  values-<env>.yaml  <  --set flags
```

### Key values to override in production

```yaml
# values-prod.yaml
collector:
  image:
    repository: hellodk/cluster-intel-collector
    tag: "7.0.0"
analyzer:
  image:
    repository: hellodk/cluster-intel-analyzer
    tag: "7.0.0"
dashboard:
  image:
    repository: hellodk/cluster-intel-dashboard
    tag: "7.0.0"

llm:
  provider: anthropic
  model: claude-sonnet-4-6
  apiKeySecret:
    name: cluster-intel-llm-secret   # create this Secret in advance
    key: api-key

stores:
  postgres:
    bundled: false                    # use managed Postgres in prod
    external:
      host: mydb.us-east-1.rds.amazonaws.com
      existingSecret: cluster-intel-pg-secret

ingress:
  enabled: true
  className: nginx
  host: cluster-intel.internal
```

### Inspect a deployed release

```bash
helm status cluster-intel -n cluster-intel
helm get values cluster-intel -n cluster-intel
helm history cluster-intel -n cluster-intel
```

### Uninstall

```bash
helm uninstall cluster-intel -n cluster-intel
# Bundled PVCs are NOT deleted — remove manually if needed:
kubectl delete pvc -n cluster-intel --all
```

### Dependency management

The dependency charts are already committed as tarballs in
`deploy/helm/cluster-intel/charts/`. If you need to update them:

```bash
helm dependency update deploy/helm/cluster-intel/
```

This updates `Chart.lock` and re-downloads the tarballs. Commit both:

```bash
git add deploy/helm/cluster-intel/Chart.lock \
        deploy/helm/cluster-intel/charts/
git commit -m "chore(helm): update bundled dependency charts"
```

---

## 4. Makefile quick reference

| Target | What it does | Key overrides |
|--------|-------------|---------------|
| `make docker-build` | Build all 3 images (`:VERSION` + `:latest`) | `REGISTRY=`, `VERSION=` |
| `make docker-push` | Push all 3 images to registry | `REGISTRY=`, `VERSION=` |
| `make helm-deps` | `helm dependency build` | — |
| `make helm-template` | Dry-render chart to stdout | `NAMESPACE=` |
| `make helm-deploy` | `helm upgrade --install` with `--wait` | `NAMESPACE=`, `ENV=`, `VALUES=` |
| `make run` | Start local stack via `run-local.sh` | `ENV=` |
| `make stop` | Stop local stack | — |
| `make status` | Status of local stack | — |
| `make doctor` | Pre-flight checks | — |
| `make build` | Compile Go binaries locally | — |
| `make test` | Run Go unit tests | — |
| `make test-e2e` | Run Playwright tests | — |

Default values (override on the CLI):

```
REGISTRY  = hellodk
VERSION   = 7.0.0
NAMESPACE = cluster-intel
ENV       = dev
```

---

## 5. First-time deployment checklist

- [ ] `docker login` with Docker Hub credentials
- [ ] `make docker-build && make docker-push` (or pull pre-built images)
- [ ] `helm lint deploy/helm/cluster-intel/` — 0 failures
- [ ] Create LLM API key Secret in cluster:
      `kubectl create secret generic cluster-intel-llm-secret \`
      `  -n cluster-intel --from-literal=api-key=<your-key>`
- [ ] Copy `values-prod.yaml.example` → `values-prod.yaml`, fill in
      real DB host, ingress host, image tags
- [ ] `helm upgrade --install cluster-intel deploy/helm/cluster-intel/ \`
      `  --namespace cluster-intel --create-namespace \`
      `  -f values-prod.yaml --wait`
- [ ] `kubectl get pods -n cluster-intel` — all Running/Ready
- [ ] `kubectl logs -n cluster-intel deploy/cluster-intel-analyzer` — no errors
