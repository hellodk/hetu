# Keycloak — Deploy Guide

Chart: **codecentric/keycloakx v7.1.11** (Keycloak app **26.5.6**)
Namespace: `keycloak`
Exposure: **plain HTTP on port 5080** via pinned ClusterIP `10.103.70.200` (NOT Traefik, NOT 80/443)
Issuer URL: `http://keycloak.local:5080/realms/hetu`

> Why not Traefik/443? Per explicit instruction, Keycloak runs on **5080**. It is
> reached directly on its ClusterIP (`10.103.70.200`), which the operator's DNS
> resolver maps to `keycloak.local`. No ingress, no TLS termination.

---

## 1. Pre-requisites

### 1a. Create the `keycloak` database in the cluster Postgres

Connect to the existing Postgres at `postgres.utilities.svc.cluster.local:5432`
(admin user: `sonar`) and create the database and a dedicated user:

```bash
kubectl run -it --rm psql-seed \
  --image=postgres:15.7-alpine \
  --restart=Never \
  -n utilities \
  -- psql "postgresql://sonar:<SONAR_PASSWORD>@postgres.utilities.svc.cluster.local:5432/postgres" \
  -c "CREATE ROLE keycloak WITH LOGIN PASSWORD 'CHOOSE_A_STRONG_PASSWORD';
      CREATE DATABASE keycloak OWNER keycloak;"
```

Replace `<SONAR_PASSWORD>` with the real sonar admin password. Replace
`CHOOSE_A_STRONG_PASSWORD` with the password you put in the SealedSecret below.

> The DB **username** (`keycloak`) is set in `values.yaml` (`database.username`).
> Only the DB **password** comes from the secret (`database.existingSecretKey: db-password`).

### 1b. Create the `keycloak-secrets` SealedSecret

```bash
# Step 1: create the plain Secret manifest (NOT committed — keep in /tmp)
kubectl create secret generic keycloak-secrets \
  --from-literal=admin-user=admin \
  --from-literal=admin-password='<ADMIN_PASSWORD>' \
  --from-literal=db-password='<DB_PASSWORD>' \
  --dry-run=client -o yaml \
  -n keycloak \
  > /tmp/keycloak-secrets.yaml

# Step 2: seal it with kubeseal
kubeseal --format yaml \
  --controller-namespace kube-system \
  --controller-name sealed-secrets \
  < /tmp/keycloak-secrets.yaml \
  > deploy/keycloak/sealed-secret.yaml

# Step 3: delete the plaintext copy immediately
rm /tmp/keycloak-secrets.yaml

# Step 4: apply the SealedSecret (controller decrypts it to a real Secret in-cluster)
kubectl apply -f deploy/keycloak/sealed-secret.yaml -n keycloak
```

`deploy/keycloak/sealed-secret.yaml` is safe to commit — encrypted ciphertext only.

**Keys the chart expects in `keycloak-secrets`:**

| Key | Used as | Description |
|-----|---------|-------------|
| `admin-user` | `KC_BOOTSTRAP_ADMIN_USERNAME` | Keycloak bootstrap admin username |
| `admin-password` | `KC_BOOTSTRAP_ADMIN_PASSWORD` | Keycloak bootstrap admin password |
| `db-password` | `database.existingSecretKey` | Postgres password for user `keycloak` |

### 1c. Create the realm ConfigMap

```bash
kubectl create namespace keycloak --dry-run=client -o yaml | kubectl apply -f -

kubectl create configmap hetu-realm \
  --from-file=realm-hetu.json=infra/hetu/keycloak/realm-hetu.json \
  -n keycloak \
  --dry-run=client -o yaml | kubectl apply -f -
```

---

## 2. Install

The chart tarball is already in the local helm library:
`/home/dk/Documents/git/dumpyard/kubernetes/utilities/helms/keycloakx-7.1.11.tgz`

```bash
helm upgrade --install keycloak \
  /home/dk/Documents/git/dumpyard/kubernetes/utilities/helms/keycloakx-7.1.11.tgz \
  -f deploy/keycloak/values.yaml \
  -n keycloak --create-namespace
```

(If you need to re-pull: `helm repo add codecentric https://codecentric.github.io/helm-charts && helm pull codecentric/keycloakx --version 7.1.11`.)

### 2a. Apply the pinned-ClusterIP Service (5080 → 10.103.70.200)

The chart Service cannot pin a clusterIP, so apply the dedicated Service:

```bash
kubectl apply -f deploy/keycloak/keycloak-service-5080.yaml
```

---

## 3. Realm import

`values.yaml` mounts the `hetu-realm` ConfigMap into `/opt/keycloak/data/import/`
and passes `--import-realm`. Keycloak imports the realm on first boot if absent.

**Verify:**

```bash
kubectl logs -n keycloak -l app.kubernetes.io/name=keycloakx | grep -i hetu
# Expected: "Realm 'hetu' imported"
```

### Issuer and JWKS URLs

| Endpoint | URL |
|----------|-----|
| Issuer / discovery | `http://keycloak.local:5080/realms/hetu/.well-known/openid-configuration` |
| JWKS (public keys) | `http://keycloak.local:5080/realms/hetu/protocol/openid-connect/certs` |
| Token endpoint | `http://keycloak.local:5080/realms/hetu/protocol/openid-connect/token` |

Chatbot backend (set in `deploy/helm/hetu/values.yaml` → `chatbot.oidc`):
```
OIDC_ISSUER=http://keycloak.local:5080/realms/hetu
OIDC_CLIENT_ID=hetu-chatbot
OIDC_AUDIENCE=hetu-chatbot
```

> **Client secret:** the realm JSON ships `REPLACE_ME_CLIENT_SECRET`. After import,
> Admin Console → Hetu realm → Clients → hetu-chatbot → Credentials → Regenerate
> Secret, then store it in the hetu app's SealedSecret.

---

## 4. DNS — operator maps the hostname

`keycloak.local` → `10.103.70.200` (the pinned ClusterIP).

This mapping is handled by the cluster DNS resolver (already configured). If a
workstation needs it in `/etc/hosts`, add it **manually**:

```
10.103.70.200  keycloak.local
```

Claude never edits `/etc/hosts` or any DNS configuration file.

---

## 5. Verification

```bash
# Discovery endpoint (plain HTTP on 5080)
curl -s http://keycloak.local:5080/realms/hetu/.well-known/openid-configuration | jq .

# Mint a token via direct-grant (directAccessGrantsEnabled: true)
curl -s -X POST \
  http://keycloak.local:5080/realms/hetu/protocol/openid-connect/token \
  -d "grant_type=password" \
  -d "client_id=hetu-chatbot" \
  -d "client_secret=<YOUR_CLIENT_SECRET>" \
  -d "username=hetu-operator" \
  -d "password=<OPERATOR_PASSWORD>" \
  | jq .access_token
```

Expected: `aud` contains `"hetu-chatbot"`, `realm_access.roles` contains `"operator"`.

---

## 6. Test user

The realm JSON includes a sample user `hetu-operator` (temporary password, forced
change on first login). Change it via the Admin Console, or:

```bash
kubectl exec -it -n keycloak \
  $(kubectl get pod -n keycloak -l app.kubernetes.io/name=keycloakx -o name | head -1) \
  -- /opt/keycloak/bin/kcadm.sh set-password \
     --server http://localhost:8080 \
     --realm hetu \
     --username hetu-operator \
     --new-password 'NEW_STRONG_PASSWORD'
```

---

## 7. Troubleshooting

| Symptom | Check |
|---------|-------|
| Pod stuck in `Init` | `kubectl describe pod -n keycloak` — dbchecker waiting on Postgres or secret missing |
| `invalid_client` on token request | Client secret mismatch — regenerate in Admin Console |
| Realm not imported | Check logs for `ERROR` during import; re-apply ConfigMap and redeploy |
| Token issuer mismatch in chatbot | `OIDC_ISSUER` must exactly equal `http://keycloak.local:5080/realms/hetu` |
| 5080 unreachable | `kubectl get svc -n keycloak keycloak-5080` — confirm clusterIP `10.103.70.200` |
