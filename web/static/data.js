// The data panel of the settings page: one file carries the whole database,
// and the same file puts it back. The panel is written for someone who is
// not technical, so it says what is inside the file, what a restore does and
// what happens when the file turns out to be wrong.

// dataState keeps the counts that were read last, so that the panel can be
// drawn again after a restore without a round trip.
let dataState = null;

// dataPanel is the markup of the panel. It is put inside the settings page.
function dataPanel(info) {
  dataState = info || dataState;
  const counts = (dataState && dataState.counts) || [];
  const total = counts.reduce((sum, row) => sum + (row.count || 0), 0);
  const safety = (dataState && dataState.safety) || [];
  return `
  <section class="panel" style="margin-top:14px">
    <div class="panel-head">
      <h2>数据备份与恢复</h2>
      <span class="muted">一个文件就是全部数据，可以随时备份，换电脑或升级前先备份一次</span>
    </div>
    <div class="backup-grid">
      <div class="backup-card">
        <h3>① 备份数据</h3>
        <p class="muted">把当前所有数据保存成一个文件。文件里有 ${counts.length} 类数据，共 <strong>${total}</strong> 条记录。</p>
        <ul class="backup-counts">${counts.map(row => `<li><span>${esc(row.label || row.table)}</span><strong>${row.count || 0}</strong></li>`).join('')}</ul>
        <div class="toolbar">
          <button id="dataExport">下载备份文件</button>
        </div>
        <p class="muted small">建议保存在网盘或另一台电脑上。文件名带下载时间，不会覆盖之前的备份。</p>
      </div>
      <div class="backup-card">
        <h3>② 恢复数据</h3>
        <p class="muted">选择之前下载的备份文件，把数据还原回去。</p>
        <div class="restore-mode">
          <label class="radio"><input type="radio" name="restoreMode" value="replace" checked><span><strong>覆盖恢复</strong><em>先清空现有数据，再写入备份里的内容。适合换电脑、重装或数据被改乱。</em></span></label>
          <label class="radio"><input type="radio" name="restoreMode" value="merge"><span><strong>合并恢复</strong><em>保留现有数据，只补上备份里没有的记录。适合把旧备份里的设备补进来。</em></span></label>
        </div>
        <div class="toolbar">
          <button id="dataImport">选择备份文件并恢复</button>
        </div>
        <p class="muted small">覆盖恢复前，系统会自动把当前数据另存一份，放在服务器数据库旁边的 backups 目录，选错文件也能找回。</p>
      </div>
    </div>
    ${safety.length ? `<div class="safety-list">
      <h3>覆盖恢复前自动保存的副本</h3>
      <p class="muted small">最近 ${safety.length} 份，最新的在前。恢复错了可以下载回来重新恢复。</p>
      <div class="table-wrap"><table><thead><tr><th>保存时间</th><th>大小</th><th>操作</th></tr></thead><tbody>
      ${safety.map(item => `<tr><td>${esc(item.created_at)}</td><td>${formatBytes(item.size)}</td><td><button class="secondary safetyDownload" data-name="${esc(item.name)}">下载</button></td></tr>`).join('')}
      </tbody></table></div>
    </div>` : ''}
  </section>`;
}

// formatBytes writes a size the way a person reads it.
function formatBytes(n) {
  n = Number(n) || 0;
  if (n < 1024) return n + ' B';
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
  return (n / 1024 / 1024).toFixed(1) + ' MB';
}

// wireDataPanel attaches the buttons of the panel. It is called after the
// settings page has been drawn.
function wireDataPanel(onRestored) {
  const exportButton = document.querySelector('#dataExport');
  if (exportButton) exportButton.onclick = () => download('/backup', '维保备份-' + timestamp() + '.json');

  const importButton = document.querySelector('#dataImport');
  if (importButton) importButton.onclick = () => pickBackupFile(onRestored);

  document.querySelectorAll('.safetyDownload').forEach(button => {
    button.onclick = () => download('/backup/safety/' + encodeURIComponent(button.dataset.name), button.dataset.name);
  });
}

// timestamp is the local time in a form that sorts well in a file name.
function timestamp() {
  const d = new Date();
  const pad = n => String(n).padStart(2, '0');
  return `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}-${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}`;
}

// pickBackupFile asks for a file, shows what it holds and only then restores
// it. The confirmation is the part that matters: a restore replaces the
// data, so the person has to see what they picked before it happens.
function pickBackupFile(onRestored) {
  const input = document.createElement('input');
  input.type = 'file';
  input.accept = '.json,application/json';
  input.onchange = async () => {
    const file = input.files[0];
    if (!file) return;
    let parsed;
    try {
      parsed = JSON.parse(await file.text());
    } catch (e) {
      alert('这个文件不是有效的备份文件，可能没有下载完整。');
      return;
    }
    const meta = parsed && parsed.meta;
    if (!meta || meta.format !== 'maintenance-reminder-backup') {
      alert('这个文件不是本系统导出的备份文件，请选择文件名以“维保备份”开头的文件。');
      return;
    }
    const mode = document.querySelector('input[name=restoreMode]:checked')?.value || 'replace';
    if (!confirmRestore(file, meta, mode)) return;
    await runRestore(file, mode, onRestored);
  };
  input.click();
}

// confirmRestore describes the file in words and asks for a confirmation.
function confirmRestore(file, meta, mode) {
  const counts = meta.counts || {};
  const lines = Object.entries(counts).map(([table, n]) => `  ${BACKUP_LABELS[table] || table}：${n}`).join('\n');
  const head = mode === 'replace'
    ? '⚠️ 覆盖恢复会清空现有数据，然后写入这个备份里的内容。'
    : '合并恢复会保留现有数据，只补上备份里缺少的记录。';
  return confirm(
    `备份文件：${file.name}\n` +
    `备份时间：${meta.created_at || '未知'}\n` +
    `由账号：${meta.created_by || '未知'} 导出\n\n` +
    `文件内容：\n${lines}\n\n` +
    head + '\n\n确定继续吗？'
  );
}

const BACKUP_LABELS = {
  push_tokens: '推送令牌',
  users: '账号',
  projects: '项目',
  devices: '设备',
  strategies: '通知策略',
  device_strategies: '设备与策略关联',
  push_templates: '推送模板',
  settings: '系统设置',
};

// runRestore uploads the file and reports what came back.
async function runRestore(file, mode, onRestored) {
  const body = new FormData();
  body.append('file', file);
  body.append('mode', mode);
  const button = document.querySelector('#dataImport');
  const label = button ? button.textContent : '';
  if (button) {
    button.disabled = true;
    button.textContent = '正在恢复，请稍候…';
  }
  try {
    const res = await fetch('/api/backup/restore', { method: 'POST', headers: { 'X-CSRF-Token': state.csrf }, body });
    const out = await res.json();
    if (!res.ok) throw Error(out.error?.message || '恢复失败');
    const restored = out.data.restored || {};
    const lines = Object.entries(restored).map(([table, n]) => `  ${BACKUP_LABELS[table] || table}：${n} 条`).join('\n');
    alert(`恢复完成。\n\n${lines}${out.data.safety_backup ? `\n\n恢复前的数据已另存为 ${out.data.safety_backup}` : ''}\n\n请刷新页面查看最新数据。`);
    if (onRestored) onRestored();
  } catch (e) {
    alert(e.message);
  } finally {
    if (button) {
      button.disabled = false;
      button.textContent = label;
    }
  }
}
