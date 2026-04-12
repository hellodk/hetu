-- 000001_init.up.sql
-- Baseline schema for cluster-intel v7 persistence layer.
-- See docs/PLAN_V7.md §5.2.1 for design rationale.

CREATE TABLE IF NOT EXISTS error_groups (
    id              BIGSERIAL PRIMARY KEY,
    fingerprint     TEXT UNIQUE NOT NULL,
    cluster_id      TEXT NOT NULL,
    service         TEXT,
    namespace       TEXT,
    title           TEXT NOT NULL,
    exception_type  TEXT,
    first_seen      TIMESTAMPTZ NOT NULL,
    last_seen       TIMESTAMPTZ NOT NULL,
    count           BIGINT NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'open',
    tags            JSONB NOT NULL DEFAULT '{}',
    ai_summary      TEXT,
    ai_summary_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT error_groups_status_check CHECK (status IN ('open', 'resolved', 'ignored'))
);

CREATE INDEX IF NOT EXISTS idx_error_groups_cluster_status
    ON error_groups (cluster_id, status, last_seen DESC);
CREATE INDEX IF NOT EXISTS idx_error_groups_tags
    ON error_groups USING GIN (tags);

-- ---

CREATE TABLE IF NOT EXISTS incidents (
    id              BIGSERIAL PRIMARY KEY,
    cluster_id      TEXT NOT NULL,
    severity        TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'open',
    detected_at     TIMESTAMPTZ NOT NULL,
    resolved_at     TIMESTAMPTZ,
    affected        JSONB NOT NULL DEFAULT '[]',
    signals         JSONB NOT NULL DEFAULT '[]',
    summary         TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT incidents_status_check CHECK (status IN ('open', 'investigating', 'resolved', 'dismissed'))
);

CREATE INDEX IF NOT EXISTS idx_incidents_cluster_status
    ON incidents (cluster_id, status, detected_at DESC);

-- ---

CREATE TABLE IF NOT EXISTS rca_reports (
    id              BIGSERIAL PRIMARY KEY,
    incident_id     BIGINT NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
    model           TEXT NOT NULL,
    prompt_tokens   INT,
    output_tokens   INT,
    confidence      REAL,
    payload         JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_rca_reports_incident
    ON rca_reports (incident_id);

-- ---

CREATE TABLE IF NOT EXISTS recommendations (
    id                          BIGSERIAL PRIMARY KEY,
    type                        TEXT NOT NULL,
    severity                    TEXT,
    confidence                  REAL,
    target                      JSONB NOT NULL DEFAULT '{}',
    current_state               JSONB,
    suggested_state             JSONB,
    rationale                   TEXT,
    ai_explanation              TEXT,
    evidence                    JSONB,
    estimated_savings_monthly   NUMERIC(12, 2),
    status                      TEXT NOT NULL DEFAULT 'open',
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT recommendations_type_check CHECK (type IN (
        'rightsizing', 'hpa', 'coredns', 'gc', 'cluster', 'scaling', 'security'
    )),
    CONSTRAINT recommendations_status_check CHECK (status IN ('open', 'accepted', 'dismissed', 'applied'))
);

CREATE INDEX IF NOT EXISTS idx_recommendations_type_status
    ON recommendations (type, status, created_at DESC);

-- ---

CREATE TABLE IF NOT EXISTS audit_log (
    id          BIGSERIAL PRIMARY KEY,
    actor       TEXT NOT NULL,
    action      TEXT NOT NULL,
    target      JSONB NOT NULL DEFAULT '{}',
    request     JSONB,
    result      TEXT NOT NULL,
    error       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_log_created
    ON audit_log (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_action
    ON audit_log (action, created_at DESC);

-- ---

CREATE TABLE IF NOT EXISTS lb_processed_objects (
    bucket          TEXT NOT NULL,
    key             TEXT NOT NULL,
    processed_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (bucket, key)
);
