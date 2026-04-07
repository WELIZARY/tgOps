-- снапшоты cron-задач и systemd-таймеров, собранных с серверов
CREATE TABLE IF NOT EXISTS cron_snapshots (
    id           SERIAL PRIMARY KEY,
    server_name  TEXT        NOT NULL,
    source       TEXT        NOT NULL,   -- crontab или systemd
    raw_output   TEXT        NOT NULL,
    collected_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cron_snapshots_server ON cron_snapshots(server_name, collected_at DESC);
