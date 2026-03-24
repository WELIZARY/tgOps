-- Результаты проверок SSL-сертификатов
CREATE TABLE ssl_checks (
    id         SERIAL PRIMARY KEY,
    domain     VARCHAR(255) NOT NULL,
    issuer     VARCHAR(255) NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ  NOT NULL,
    days_left  INTEGER      NOT NULL,
    status     VARCHAR(20)  NOT NULL, -- ok, warning, critical, expired
    checked_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX idx_ssl_checks_domain ON ssl_checks(domain, checked_at DESC);
