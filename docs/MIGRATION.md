# Migration — from `manifests/` + `deploy.sh` to Helm-only

## Summary

As of chart `0.2.0` (appVersion `7.0.0`), **all deployments go through
Helm**. The legacy kustomize manifests at `manifests/` and the `deploy.sh`
wrapper have been removed. Everything they did is now either a template
in the chart or a `values.yaml` toggle.

## What changed

| Before | After |
|---|---|
| `./deploy.sh` | `make helm-deploy` or `helm upgrade --install cluster-intel deploy/helm/cluster-intel/ -f values-deploy.yaml` |
| `kubectl apply -k manifests/base/` | `helm upgrade --install cluster-intel deploy/helm/cluster-intel/` |
| `kubectl apply -k manifests/overlays/production/` | `helm upgrade --install … -f values-deploy.yaml` |
| `./manifests/monitoring/deploy-monitoring.sh` | `helm upgrade --install … --set monitoring.enabled=true --set monitoring.ollama.externalIP=<your-ip>` |
| NetworkPolicy lived in `manifests/base/network-policies.yaml` and was always applied | Now `--set networkPolicy.enabled=true` (opt-in). CIDRs, peer namespaces, LLM CIDR all configurable |
| PDB, securityContext, anti-affinity were kustomize-only | Now in every deployment template, gated by `<component>.pdb.enabled` / `<component>.securityContext` |
| ClickHouse block in values.yaml | Removed — was never wired. Re-add when a CH-backed feature ships |

## Day-1 install

```bash
make helm-deps                     # builds sub-chart tarballs (once)
make helm-deploy                   # = helm upgrade --install …
```

Or manually with a production override:

```bash
helm dependency build deploy/helm/cluster-intel/
helm upgrade --install cluster-intel deploy/helm/cluster-intel/ \
  --namespace cluster-intel --create-namespace \
  -f values-deploy.yaml
```

## Enabling optional features

### NetworkPolicy (zero-trust)

```bash
--set networkPolicy.enabled=true \
--set networkPolicy.llmEgressCIDR=10.0.0.50/32 \
--set networkPolicy.llmEgressPort=11434
```

Requires a CNI that enforces NetworkPolicy (Calico, Cilium, Antrea, etc.).

### PodDisruptionBudgets

```bash
--set collector.pdb.enabled=true --set collector.pdb.minAvailable=1 \
--set analyzer.pdb.enabled=true  --set analyzer.pdb.minAvailable=1 \
--set dashboard.pdb.enabled=true --set dashboard.pdb.minAvailable=1
```

Only enable when the matching component has `replicas >= 2` — a PDB with
`minAvailable: 1` on a 1-replica deployment blocks drain.

### Observability stack

```bash
--set monitoring.enabled=true \
--set monitoring.ollama.externalIP=10.0.0.50   # required when ollama.enabled=true
```

Deploys Tempo + OTEL Collector + ServiceMonitor for your external Ollama
+ PrometheusRule with 14 alerts + Grafana dashboards for Cluster Intel,
Ollama, and LLM traces.

Enable Slack routing separately:

```bash
kubectl create secret generic alertmanager-slack \
  -n monitoring \
  --from-literal=slack-webhook-url=https://hooks.slack.com/services/XXX/YYY/ZZZ

--set monitoring.alertmanager.enabled=true \
--set monitoring.alertmanager.slackWebhookSecret.name=alertmanager-slack
```

## Rolling upgrade from a 0.1.x install

The new templates are additive and gated. Running `helm upgrade` from
`0.1.x` to `0.2.0` with your existing values.yaml will:

1. Leave your existing deployments unchanged (no new resources by default).
2. Add a container-level `securityContext` block to each deployment —
   this may trigger a rolling restart of pods.
3. Do NOT create NetworkPolicies, PDBs, or monitoring resources unless
   explicitly enabled.

If you were previously applying `manifests/base/network-policies.yaml`
via kustomize and want to keep that behaviour, set
`networkPolicy.enabled=true` on the first upgrade.

## Uninstalling

```bash
helm uninstall cluster-intel -n cluster-intel
```

Note: the bundled Postgres/Redis/NATS PVCs are retained by default (a
safety feature of those sub-charts). To also delete persistent data:

```bash
kubectl delete pvc -n cluster-intel -l app.kubernetes.io/instance=cluster-intel
kubectl delete namespace cluster-intel   # if you want the ns gone too
```

## What's gone

- `deploy.sh` — replaced by `make helm-deploy` / `helm upgrade`
- `manifests/` (entire directory)
- `scripts/pre-deploy-check.sh` — referenced removed manifest paths
- `scripts/bump-version.sh` — same
