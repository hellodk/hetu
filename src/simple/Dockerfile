FROM python:3.11-slim

LABEL maintainer="cluster-intel" \
      description="K8s Cluster Intelligence Engine - Real-time cluster health monitoring and analysis" \
      version="6.0"

WORKDIR /app

COPY requirements.txt /app/
RUN pip install --no-cache-dir -r requirements.txt && \
    apt-get update && apt-get install -y --no-install-recommends curl && \
    rm -rf /var/lib/apt/lists/*

COPY app.py /app/
COPY ui.html /app/

ENV PYTHONUNBUFFERED=1 \
    PORT=8080 \
    LLM_ENABLED=false \
    LLM_BASE_URL="" \
    LLM_MODEL="llama3.2" \
    PROMETHEUS_URL="" \
    SLACK_WEBHOOK="" \
    TEAMS_WEBHOOK=""

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD curl -f http://localhost:8080/api/v1/health || exit 1

USER 1000

CMD ["python", "app.py"]
