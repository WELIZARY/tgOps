-- таблица для хранения истории запусков Ansible-плейбуков
CREATE TABLE IF NOT EXISTS ansible_runs (
    id            SERIAL PRIMARY KEY,
    playbook_name TEXT        NOT NULL,
    playbook_file TEXT        NOT NULL,
    started_by    INTEGER     NOT NULL REFERENCES users(id),
    status        TEXT        NOT NULL DEFAULT 'running',
    output        TEXT        NOT NULL DEFAULT '',
    started_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at   TIMESTAMPTZ,
    duration_ms   INTEGER     NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_ansible_runs_started_at ON ansible_runs(started_at DESC);
