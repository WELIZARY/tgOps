-- Аудит-лог всех действий пользователей
CREATE TABLE audit_log (
    id          SERIAL PRIMARY KEY,
    user_id     INTEGER      REFERENCES users(id),
    command     VARCHAR(255) NOT NULL,
    args        TEXT         NOT NULL DEFAULT '',
    result      VARCHAR(20)  NOT NULL, -- success, error, denied
    duration_ms INTEGER      NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_log_created_at ON audit_log(created_at DESC);
CREATE INDEX idx_audit_log_user_id    ON audit_log(user_id);
