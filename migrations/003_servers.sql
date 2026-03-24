-- Управляемые серверы
CREATE TABLE servers (
    id         SERIAL PRIMARY KEY,
    name       VARCHAR(255) UNIQUE NOT NULL,
    host       VARCHAR(255) NOT NULL,
    port       INTEGER      NOT NULL DEFAULT 22,
    ssh_user   VARCHAR(255) NOT NULL,
    is_active  BOOLEAN      NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);
