"""
Keycloak OIDC authentication dependency for the chatbot.

Env vars:
  OIDC_ISSUER      — e.g. https://keycloak.local/realms/hetu  (required when AUTH_ENABLED=true)
  OIDC_CLIENT_ID   — default: hetu-chatbot
  OIDC_AUDIENCE    — default: value of OIDC_CLIENT_ID
  AUTH_ENABLED     — default: true  (set to "false"/"0"/"" to bypass for local dev)
"""

import os
import logging
from typing import Any

import jwt  # pyjwt[crypto]
from jwt import PyJWKClient, PyJWKClientError
from fastapi import Header, HTTPException
from typing import Optional

logger = logging.getLogger(__name__)

# ---------------------------------------------------------------------------
# Runtime config (read once at import time; tests may monkeypatch os.environ
# before importing or patch the module-level variables directly)
# ---------------------------------------------------------------------------

def _is_auth_enabled() -> bool:
    val = os.environ.get("AUTH_ENABLED", "true").strip().lower()
    return val not in ("false", "0", "")


def _get_issuer() -> str:
    return os.environ.get("OIDC_ISSUER", "").strip()


def _get_client_id() -> str:
    return os.environ.get("OIDC_CLIENT_ID", "hetu-chatbot").strip()


def _get_audience() -> str:
    return os.environ.get("OIDC_AUDIENCE", _get_client_id()).strip()


# JWKS client cache: keyed by issuer URL so a changed OIDC_ISSUER gets a fresh client
_jwks_clients: dict[str, PyJWKClient] = {}


def _get_jwks_client(issuer: str) -> PyJWKClient:
    if issuer not in _jwks_clients:
        jwks_uri = f"{issuer}/protocol/openid-connect/certs"
        _jwks_clients[issuer] = PyJWKClient(jwks_uri, cache_keys=True)
    return _jwks_clients[issuer]


# Stub principal returned when auth is disabled
_STUB_PRINCIPAL: dict[str, Any] = {
    "sub": "dev-user",
    "preferred_username": "dev-user",
    "auth_disabled": True,
}


async def require_token(
    authorization: Optional[str] = Header(None),
) -> dict[str, Any]:
    """FastAPI dependency — validates the Bearer token and returns the decoded claims.

    If AUTH_ENABLED is falsey, returns a stub principal without any network call.
    Raises HTTP 401 on any auth failure.
    """
    if not _is_auth_enabled():
        return _STUB_PRINCIPAL

    issuer = _get_issuer()
    if not issuer:
        logger.error("OIDC_ISSUER is not set but AUTH_ENABLED=true")
        raise HTTPException(status_code=500, detail="Server auth misconfiguration")

    if not authorization or not authorization.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="Missing or invalid Authorization header")

    token = authorization[len("Bearer "):]

    try:
        jwks_client = _get_jwks_client(issuer)
        try:
            signing_key = jwks_client.get_signing_key_from_jwt(token)
        except PyJWKClientError:
            # kid miss — clear cache and retry once
            _jwks_clients.pop(issuer, None)
            jwks_client = _get_jwks_client(issuer)
            signing_key = jwks_client.get_signing_key_from_jwt(token)

        audience = _get_audience()
        claims = jwt.decode(
            token,
            signing_key.key,
            algorithms=["RS256", "ES256"],
            audience=audience,
            issuer=issuer,
            options={"verify_exp": True},
        )
        return claims

    except jwt.ExpiredSignatureError:
        raise HTTPException(status_code=401, detail="Token expired")
    except jwt.InvalidIssuerError:
        raise HTTPException(status_code=401, detail="Invalid token issuer")
    except jwt.InvalidAudienceError:
        raise HTTPException(status_code=401, detail="Invalid token audience")
    except jwt.PyJWTError as exc:
        logger.warning("JWT validation failed: %s", exc)
        raise HTTPException(status_code=401, detail="Invalid token")
    except Exception as exc:
        logger.error("Unexpected auth error: %s", exc)
        raise HTTPException(status_code=401, detail="Authentication error")
