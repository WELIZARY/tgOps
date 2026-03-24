-- События CI/CD пайплайнов
CREATE TABLE pipeline_events (
    id          SERIAL PRIMARY KEY,
    pipeline_id VARCHAR(255) NOT NULL,
    source      VARCHAR(50)  NOT NULL,   -- github, gitlab, jenkins
    repo        VARCHAR(255) NOT NULL DEFAULT '',
    branch      VARCHAR(255) NOT NULL DEFAULT '',
    status      VARCHAR(50)  NOT NULL,   -- pending, running, success, failed
    author      VARCHAR(255) NOT NULL DEFAULT '',
    approved_by INTEGER      REFERENCES users(id),
    approved_at TIMESTAMPTZ,
    payload     JSONB,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_pipeline_events_created_at ON pipeline_events(created_at DESC);
CREATE INDEX idx_pipeline_events_status     ON pipeline_events(status);
