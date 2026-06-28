"""
Tests: env-var config resolution in load_config().

Resolution order per field: env var → YAML file value → hardcoded default.
"""

import importlib
import os
import sys

import pytest


def _reload_main():
    """Reload main so load_config() picks up current os.environ."""
    # Remove cached module so the module-level load_config() call re-runs
    for mod in list(sys.modules.keys()):
        if mod == "main" or mod.startswith("main."):
            del sys.modules[mod]
    import main as m
    return m


class TestLoadConfigEnvVars:
    def test_llm_endpoint_from_env(self, monkeypatch, tmp_path):
        monkeypatch.setenv("LLM_ENDPOINT", "http://myhost:11434/v1/chat/completions")
        monkeypatch.setenv("LLM_MODEL", "llama3")
        monkeypatch.chdir(tmp_path)  # no config/chatbot-models.yaml here

        m = _reload_main()
        cfg = m.load_config()

        assert cfg.llm_endpoint == "http://myhost:11434/v1/chat/completions"
        assert cfg.llm_model == "llama3"

    def test_embedding_endpoint_from_env(self, monkeypatch, tmp_path):
        monkeypatch.setenv("EMBEDDING_ENDPOINT", "http://embed-host:8080/embeddings")
        monkeypatch.setenv("EMBEDDING_MODEL", "nomic-embed-text")
        monkeypatch.chdir(tmp_path)

        m = _reload_main()
        cfg = m.load_config()

        assert cfg.embedding_endpoint == "http://embed-host:8080/embeddings"
        assert cfg.embedding_model == "nomic-embed-text"

    def test_llm_provider_from_env(self, monkeypatch, tmp_path):
        monkeypatch.setenv("LLM_PROVIDER", "openai")
        monkeypatch.chdir(tmp_path)

        m = _reload_main()
        cfg = m.load_config()

        assert cfg.llm_provider == "openai"

    def test_defaults_when_no_env_no_yaml(self, monkeypatch, tmp_path):
        # Strip all relevant env vars
        for key in ("LLM_ENDPOINT", "LLM_MODEL", "LLM_PROVIDER", "EMBEDDING_ENDPOINT", "EMBEDDING_MODEL"):
            monkeypatch.delenv(key, raising=False)
        monkeypatch.chdir(tmp_path)

        m = _reload_main()
        cfg = m.load_config()

        # Hardcoded defaults must still be present
        assert "192.168.1.19" in cfg.llm_endpoint or cfg.llm_endpoint  # non-empty
        assert cfg.llm_model  # non-empty
        assert cfg.llm_provider == "ollama"  # default provider

    def test_env_overrides_yaml(self, monkeypatch, tmp_path):
        # Write a YAML file with different values
        config_dir = tmp_path / "config"
        config_dir.mkdir()
        yaml_content = """
llm:
  local:
    endpoint: "http://yaml-host:8080/v1/chat/completions"
    model_name: "yaml-model"
    timeout_seconds: 30
embedding:
  local:
    endpoint: "http://yaml-embed:8080/embeddings"
    model_name: "yaml-embed-model"
    timeout_seconds: 10
"""
        (config_dir / "chatbot-models.yaml").write_text(yaml_content)
        monkeypatch.chdir(tmp_path)

        # Set env var — it should win over YAML
        monkeypatch.setenv("LLM_ENDPOINT", "http://env-host:9999/v1/chat/completions")
        monkeypatch.setenv("LLM_MODEL", "env-model")

        m = _reload_main()
        cfg = m.load_config()

        assert cfg.llm_endpoint == "http://env-host:9999/v1/chat/completions"
        assert cfg.llm_model == "env-model"

    def test_yaml_used_when_no_env(self, monkeypatch, tmp_path):
        for key in ("LLM_ENDPOINT", "LLM_MODEL", "LLM_PROVIDER", "EMBEDDING_ENDPOINT", "EMBEDDING_MODEL"):
            monkeypatch.delenv(key, raising=False)

        config_dir = tmp_path / "config"
        config_dir.mkdir()
        yaml_content = """
llm:
  local:
    endpoint: "http://yaml-only:8080/v1/chat/completions"
    model_name: "yaml-only-model"
    timeout_seconds: 45
embedding:
  local:
    endpoint: "http://yaml-embed:8080/embeddings"
    model_name: "yaml-embed-model"
    timeout_seconds: 15
"""
        (config_dir / "chatbot-models.yaml").write_text(yaml_content)
        monkeypatch.chdir(tmp_path)

        m = _reload_main()
        cfg = m.load_config()

        assert cfg.llm_endpoint == "http://yaml-only:8080/v1/chat/completions"
        assert cfg.llm_model == "yaml-only-model"
        assert cfg.llm_timeout == 45
