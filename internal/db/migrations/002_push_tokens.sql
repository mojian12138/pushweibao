CREATE TABLE IF NOT EXISTS push_tokens (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'user' CHECK(kind IN ('user','message','group')),
  token_value TEXT NOT NULL,
  topic_code TEXT NOT NULL DEFAULT '',
  channel TEXT NOT NULL DEFAULT 'wechat',
  template TEXT NOT NULL DEFAULT 'html',
  option TEXT NOT NULL DEFAULT '',
  callback_url TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  note TEXT NOT NULL DEFAULT '',
  expires_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_push_tokens_enabled ON push_tokens(enabled);

ALTER TABLE users ADD COLUMN push_token_id INTEGER REFERENCES push_tokens(id) ON DELETE SET NULL;
ALTER TABLE strategies ADD COLUMN token_id INTEGER REFERENCES push_tokens(id) ON DELETE SET NULL;
ALTER TABLE notification_outbox ADD COLUMN token_id INTEGER REFERENCES push_tokens(id) ON DELETE SET NULL;
ALTER TABLE notification_outbox ADD COLUMN token_name TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_outbox ADD COLUMN channel_detail TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_outbox ADD COLUMN msg_id TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_attempts ADD COLUMN code TEXT NOT NULL DEFAULT '';
