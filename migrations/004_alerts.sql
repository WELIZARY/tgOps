-- История алертов
CREATE TABLE alerts (
    id           SERIAL PRIMARY KEY,
    server_name  VARCHAR(255) NOT NULL,
    alert_type   VARCHAR(50)  NOT NULL,  -- cpu, ram, disk, service_down, ssl
    severity     VARCHAR(20)  NOT NULL,  -- critical, warning, info
    message      TEXT         NOT NULL,
    acknowledged BOOLEAN      NOT NULL DEFAULT false,
    ack_by       INTEGER      REFERENCES users(id),
    ack_at       TIMESTAMPTZ,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_alerts_created_at    ON alerts(created_at DESC);
CREATE INDEX idx_alerts_unacked       ON alerts(acknowledged) WHERE NOT acknowledged;
