-- Добавляем key_name в servers: имя файла приватного ключа в директории keys/
-- Пустое значение означает использование default_key_path из конфига
ALTER TABLE servers ADD COLUMN IF NOT EXISTS key_name VARCHAR(255) NOT NULL DEFAULT '';
