-- The trigger of a strategy gains a cron expression, so that a summary can
-- be sent on any schedule, not only once a month. A strategy with an empty
-- expression keeps the trigger of its type.
ALTER TABLE strategies ADD COLUMN cron_expr TEXT NOT NULL DEFAULT '';

-- A strategy can point at the template it sends, so the words and the
-- triggers are managed apart. A strategy without one keeps rendering its
-- own text, which keeps the strategies of the earlier releases working as
-- they are.
ALTER TABLE strategies ADD COLUMN template_id INTEGER NOT NULL DEFAULT 0;

-- The message templates are managed on a page of their own, so a strategy
-- only points at the template it sends. A strategy without one keeps
-- rendering its own text, which keeps the strategies of the earlier
-- releases working as they are.
CREATE TABLE IF NOT EXISTS push_templates (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'milestone' CHECK(kind IN ('milestone','monthly','cycle')),
  format TEXT NOT NULL DEFAULT 'markdown',
  title_template TEXT NOT NULL DEFAULT '',
  body_template TEXT NOT NULL DEFAULT '',
  item_template TEXT NOT NULL DEFAULT '',
  builtin INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- The three built-in templates the program starts with. They carry the
-- detailed window layout: the tiers of the devices inside the horizon,
-- grouped and sorted by urgency.
INSERT INTO push_templates(name,kind,format,title_template,body_template,item_template,builtin) VALUES
('设备维保到期提醒','milestone','markdown',
 '【维保提醒】剩余 {{days}} 天 · 共 {{count}} 台',
 '## 📌 维保到期提醒

{{milestone_items}}

> 请及时联系客户办理续保。',
 '', 1),
('月度维保汇总','monthly','markdown',
 '【月度维保汇总】{{month}}',
 '## 📅 {{month}} 维保到期汇总

本月共有 **{{count}}** 台设备即将到期。

{{upcoming_section}}

{{expired_section}}

> 请及时核对设备状态，安排续保。',
 '| {{project}} | {{model}} | {{sn}} | {{end}} | **{{days}} 天** | {{owner}} |', 1),
('周期维保提醒','cycle','markdown',
 '【维保提醒】剩余 {{days}} 天',
 '## 🔁 周期维保提醒

| 项目 | {{project}} |
| :--- | :--- |
| 客户 | {{customer}} |
| 设备 | {{vendor}} {{model}} |
| SN | {{sn}} |
| 到期 | {{end}} |
| **剩余** | **{{days}} 天** |
| 负责人 | {{owner}} |

> 请及时办理续保。',
 '', 1);
