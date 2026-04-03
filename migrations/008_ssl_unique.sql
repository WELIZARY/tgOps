-- Уникальный индекс на домен для upsert в ssl_checks
CREATE UNIQUE INDEX IF NOT EXISTS idx_ssl_checks_domain_unique ON ssl_checks(domain);
