-- The title of a reminder becomes a template, so it can be customised the
-- same way the body already is.
ALTER TABLE strategies ADD COLUMN title_template TEXT NOT NULL DEFAULT '';

-- The layout of one line inside the monthly summary, so that the summary
-- can be reformatted without touching the code.
ALTER TABLE strategies ADD COLUMN item_template TEXT NOT NULL DEFAULT '';

-- The rendered title is stored with the message, because the template that
-- produced it may be edited later.
ALTER TABLE notification_outbox ADD COLUMN title TEXT NOT NULL DEFAULT '';

-- A system log records what happened across the whole application, so that
-- the log page can show more than the push attempts.
CREATE TABLE IF NOT EXISTS system_logs (
  id INTEGER PRIMARY KEY,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  level TEXT NOT NULL DEFAULT 'info' CHECK(level IN ('debug','info','warn','error')),
  category TEXT NOT NULL DEFAULT 'system',
  action TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '',
  actor TEXT NOT NULL DEFAULT '',
  ref_type TEXT NOT NULL DEFAULT '',
  ref_id INTEGER NOT NULL DEFAULT 0,
  ip TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_logs_created ON system_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_logs_category ON system_logs(category);
CREATE INDEX IF NOT EXISTS idx_logs_level ON system_logs(level);
