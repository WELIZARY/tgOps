-- добавить поле для хранения Telegram message_id уведомления о деплое
-- нужно для редактирования сообщения при approve/reject
ALTER TABLE pipeline_events
    ADD COLUMN IF NOT EXISTS tg_message_id INTEGER NOT NULL DEFAULT 0;
