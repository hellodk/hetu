"""
Tests: auth.py — require_token FastAPI dependency.

Uses a locally-generated RSA key to produce real JWTs.
PyJWKClient is monkeypatched so no network calls are made.
"""

import importlib
import os
import sys
import time
from typing import Any
from unittest.mock import MagicMock, patch

import pytest

# ---------------------------------------------------------------------------
# Helpers to generate real RSA-signed JWTs
# ---------------------------------------------------------------------------

from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.hazmat.backends import default_backend
import jwt as _jwt  # pyjwt[crypto]


def _generate_rsa_key():
    private_key = rsa.generate_private_key(
        public_exponent=65537,
        key_size=2048,
        backend=default_backend(),
    )
    return private_key


def _make_token(
    private_key,
    *,
    iss: str = "https://keycloak.local/realms/hetu",
    aud: str = "hetu-chatbot",
    sub: str = "user-123",
    exp_offset: int = 3600,
    algorithm: str = "RS256",
) -> str:
    payload = {
        "iss": iss,
        "aud": aud,
        "sub": sub,
        "iat": int(time.time()),
        "exp": int(time.time()) + exp_offset,
    }
    return _jwt.encode(payload, private_key, algorithm=algorithm)


def _reload_auth():
    for mod in list(sys.modules.keys()):
        if mod == "auth" or mod.startswith("auth."):
            del sys.modules[mod]
    import auth as a
    return a


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

ISSUER = "https://keycloak.local/realms/hetu"
CLIENT_ID = "hetu-chatbot"


@pytest.fixture(autouse=True)
def _clear_auth_module():
    """Ensure a fresh auth module for each test."""
    yield
    for mod in list(sys.modules.keys()):
        if mod == "auth" or mod.startswith("auth."):
            del sys.modules[mod]


class TestAuthDisabled:
    def test_bypass_returns_stub(self, monkeypatch):
        monkeypatch.setenv("AUTH_ENABLED", "false")
        import auth as a

        import asyncio
        # Call the dependency directly (no Header injection needed here)
        result = asyncio.get_event_loop().run_until_complete(
            a.require_token(authorization=None)
        )
        assert result.get("auth_disabled") is True

    def test_bypass_with_zero(self, monkeypatch):
        monkeypatch.setenv("AUTH_ENABLED", "0")
        import auth as a
        import asyncio
        result = asyncio.get_event_loop().run_until_complete(
            a.require_token(authorization=None)
        )
        assert result.get("auth_disabled") is True

    def test_bypass_with_empty_string(self, monkeypatch):
        monkeypatch.setenv("AUTH_ENABLED", "")
        import auth as a
        import asyncio
        result = asyncio.get_event_loop().run_until_complete(
            a.require_token(authorization=None)
        )
        assert result.get("auth_disabled") is True


class TestAuthEnabled:
    @pytest.fixture(autouse=True)
    def _set_env(self, monkeypatch):
        monkeypatch.setenv("AUTH_ENABLED", "true")
        monkeypatch.setenv("OIDC_ISSUER", ISSUER)
        monkeypatch.setenv("OIDC_CLIENT_ID", CLIENT_ID)
        monkeypatch.setenv("OIDC_AUDIENCE", CLIENT_ID)

    def test_missing_header_raises_401(self):
        import auth as a
        import asyncio
        from fastapi import HTTPException
        with pytest.raises(HTTPException) as exc_info:
            asyncio.get_event_loop().run_until_complete(
                a.require_token(authorization=None)
            )
        assert exc_info.value.status_code == 401

    def test_non_bearer_raises_401(self):
        import auth as a
        import asyncio
        from fastapi import HTTPException
        with pytest.raises(HTTPException) as exc_info:
            asyncio.get_event_loop().run_until_complete(
                a.require_token(authorization="Basic dXNlcjpwYXNz")
            )
        assert exc_info.value.status_code == 401

    def test_garbage_token_raises_401(self):
        import auth as a
        import asyncio
        from fastapi import HTTPException

        private_key = _generate_rsa_key()
        # Monkeypatch PyJWKClient so it doesn't hit the network
        mock_jwks = MagicMock()
        mock_signing_key = MagicMock()
        mock_signing_key.key = private_key.public_key()
        mock_jwks.get_signing_key_from_jwt.return_value = mock_signing_key

        with patch("auth.PyJWKClient", return_value=mock_jwks):
            with pytest.raises(HTTPException) as exc_info:
                asyncio.get_event_loop().run_until_complete(
                    a.require_token(authorization="Bearer not.a.valid.jwt")
                )
        assert exc_info.value.status_code == 401

    def test_wrong_issuer_raises_401(self):
        import auth as a
        import asyncio
        from fastapi import HTTPException

        private_key = _generate_rsa_key()
        token = _make_token(private_key, iss="https://evil.example.com/realms/bad")

        mock_jwks = MagicMock()
        mock_signing_key = MagicMock()
        mock_signing_key.key = private_key.public_key()
        mock_jwks.get_signing_key_from_jwt.return_value = mock_signing_key

        with patch("auth.PyJWKClient", return_value=mock_jwks):
            with patch("auth._get_jwks_client", return_value=mock_jwks):
                with pytest.raises(HTTPException) as exc_info:
                    asyncio.get_event_loop().run_until_complete(
                        a.require_token(authorization=f"Bearer {token}")
                    )
        assert exc_info.value.status_code == 401

    def test_expired_token_raises_401(self):
        import auth as a
        import asyncio
        from fastapi import HTTPException

        private_key = _generate_rsa_key()
        token = _make_token(private_key, iss=ISSUER, aud=CLIENT_ID, exp_offset=-10)

        mock_jwks = MagicMock()
        mock_signing_key = MagicMock()
        mock_signing_key.key = private_key.public_key()
        mock_jwks.get_signing_key_from_jwt.return_value = mock_signing_key

        with patch("auth._get_jwks_client", return_value=mock_jwks):
            with pytest.raises(HTTPException) as exc_info:
                asyncio.get_event_loop().run_until_complete(
                    a.require_token(authorization=f"Bearer {token}")
                )
        assert exc_info.value.status_code == 401

    def test_valid_token_returns_claims(self):
        import auth as a
        import asyncio

        private_key = _generate_rsa_key()
        token = _make_token(private_key, iss=ISSUER, aud=CLIENT_ID)

        mock_jwks = MagicMock()
        mock_signing_key = MagicMock()
        mock_signing_key.key = private_key.public_key()
        mock_jwks.get_signing_key_from_jwt.return_value = mock_signing_key

        with patch("auth._get_jwks_client", return_value=mock_jwks):
            claims = asyncio.get_event_loop().run_until_complete(
                a.require_token(authorization=f"Bearer {token}")
            )

        assert claims["iss"] == ISSUER
        assert claims["sub"] == "user-123"

    def test_wrong_audience_raises_401(self):
        import auth as a
        import asyncio
        from fastapi import HTTPException

        private_key = _generate_rsa_key()
        token = _make_token(private_key, iss=ISSUER, aud="wrong-client")

        mock_jwks = MagicMock()
        mock_signing_key = MagicMock()
        mock_signing_key.key = private_key.public_key()
        mock_jwks.get_signing_key_from_jwt.return_value = mock_signing_key

        with patch("auth._get_jwks_client", return_value=mock_jwks):
            with pytest.raises(HTTPException) as exc_info:
                asyncio.get_event_loop().run_until_complete(
                    a.require_token(authorization=f"Bearer {token}")
                )
        assert exc_info.value.status_code == 401
