PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL CHECK(role IN ('admin','viewer')),
  active INTEGER NOT NULL DEFAULT 1,
  must_change_password INTEGER NOT NULL DEFAULT 0,
  pushplus_token TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS sessions (
  token_hash TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  csrf_token TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS projects (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  customer TEXT NOT NULL DEFAULT '',
  contact TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  contract_no TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS devices (
  id INTEGER PRIMARY KEY,
  project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  vendor TEXT NOT NULL,
  model TEXT NOT NULL,
  serial_number TEXT NOT NULL,
  maintenance_start TEXT NOT NULL,
  maintenance_end TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active','renewed','retired')),
  notes TEXT NOT NULL DEFAULT '',
  owner TEXT NOT NULL DEFAULT '',
  contract_no TEXT NOT NULL DEFAULT '',
  strategy_override INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(project_id, serial_number)
);
CREATE INDEX IF NOT EXISTS idx_devices_project ON devices(project_id);
CREATE INDEX IF NOT EXISTS idx_devices_end ON devices(maintenance_end);
CREATE TABLE IF NOT EXISTS strategies (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  type TEXT NOT NULL CHECK(type IN ('milestone','monthly','cycle')),
  enabled INTEGER NOT NULL DEFAULT 1,
  sort_order INTEGER NOT NULL DEFAULT 0,
  offsets TEXT NOT NULL DEFAULT '[90,60,30,7,0]',
  monthly_day INTEGER NOT NULL DEFAULT 1,
  horizon_days INTEGER NOT NULL DEFAULT 90,
  cycle_threshold INTEGER NOT NULL DEFAULT 30,
  cycle_interval INTEGER NOT NULL DEFAULT 7,
  template TEXT NOT NULL DEFAULT '',
  recipients TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS device_strategies (
  device_id INTEGER NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  strategy_id INTEGER NOT NULL REFERENCES strategies(id) ON DELETE CASCADE,
  PRIMARY KEY(device_id, strategy_id)
);
CREATE TABLE IF NOT EXISTS notification_outbox (
  id INTEGER PRIMARY KEY,
  dedupe_key TEXT NOT NULL UNIQUE,
  device_id INTEGER REFERENCES devices(id) ON DELETE SET NULL,
  project_name TEXT NOT NULL DEFAULT '',
  strategy_name TEXT NOT NULL DEFAULT '',
  trigger_point TEXT NOT NULL DEFAULT '',
  channel TEXT NOT NULL DEFAULT 'PushPlus',
  recipient TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','sending','sent','failed')),
  attempts INTEGER NOT NULL DEFAULT 0,
  next_attempt TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  sent_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_outbox_pending ON notification_outbox(status,next_attempt);
CREATE TABLE IF NOT EXISTS notification_attempts (
  id INTEGER PRIMARY KEY,
  outbox_id INTEGER NOT NULL REFERENCES notification_outbox(id) ON DELETE CASCADE,
  succeeded INTEGER NOT NULL,
  detail TEXT NOT NULL DEFAULT '',
  attempted_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL DEFAULT '');

INSERT OR IGNORE INTO strategies(name,type,enabled,sort_order,offsets,template)
VALUES('默认里程碑提醒','milestone',1,10,'[90,60,30,7,0]','【维保提醒】剩余 {{days}} 天｜项目：{{project}}｜客户：{{customer}}｜设备：{{vendor}} {{model}}｜SN：{{sn}}｜到期：{{end}}｜负责人：{{owner}}');
INSERT OR IGNORE INTO strategies(name,type,enabled,sort_order,monthly_day,horizon_days,template)
VALUES('月度汇总','monthly',1,20,1,90,'【月度维保汇总】{{month}}：{{count}} 台设备即将到期\n{{items}}');
INSERT OR IGNORE INTO strategies(name,type,enabled,sort_order,cycle_threshold,cycle_interval,template)
VALUES('周期循环提醒','cycle',0,30,30,7,'【维保提醒】项目：{{project}}｜{{vendor}} {{model}}（SN：{{sn}}）剩余 {{days}} 天，请及时续保。');
