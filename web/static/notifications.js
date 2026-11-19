// The notification page. It holds the two blocks the notification work is
// made of: the strategies, each one a trigger plus the template it sends,
// and the message templates themselves, with the words the reminders are
// written in.
//
// The editor of a strategy only asks for what a strategy is: a name, a
// trigger and the template it sends. The recipients and the token are
// under "更多选项", because most strategies share the defaults.

// The visual choices of a cron trigger, so a schedule can be picked
// without writing the expression.
const CRON_PRESETS = [
  {expr:'0 9 * * *', label:'每天 09:00'},
  {expr:'0 9 1 * *', label:'每月 1 日 09:00'},
  {expr:'0 9 * * 1', label:'每周一 09:00'},
  {expr:'0 9,18 * * *', label:'每天 09:00 与 18:00'},
  {expr:'@hourly', label:'每小时整点'},
  {expr:'*/30 * * * *', label:'每 30 分钟'},
];

// The plain words of a trigger, for the card of a strategy. A monthly
// strategy without an expression is one of the earlier releases: it fires
// on the first check of its day, so the card says so instead of inventing
// a time.
function triggerText(x){
  if(x.cron_expr){
    const d=x.cron_describe||x.cron_expr;
    return '定时推送：'+d;
  }
  if(x.type==='milestone')return '设备进入 '+x.offsets.join('、')+' 天窗口时推送';
  if(x.type==='monthly')return `每月 ${x.monthly_day} 日当天首次检查时推送`;
  return `到期前 ${x.cycle_threshold} 天起，每 ${x.cycle_interval} 天提醒一次`;
}

async function strategies(){
  const [xs,ts,tk]=await Promise.all([pageData('/strategies'),pageData('/push-templates'),pageData('/push-tokens')]);
  if(!xs)return;
  const admin=state.user.role==='admin';
  const tokenName=id=>((tk||[]).find(t=>t.id===id)||{}).name||'默认令牌';
  shell(`<h1>通知策略</h1>
  <section class="panel">
    <div class="panel-head"><h2>推送策略</h2><span class="muted">一条策略＝什么时候推＋推什么内容</span></div>
    <div class="toolbar">
      ${admin?'<button id="add">＋ 新增策略</button>':''}
      <span class="muted">共 ${xs.length} 条</span>
    </div>
    <div id="rows" class="strategy-list">${xs.length?xs.map(x=>`<section class="panel strategy-card" data-id="${x.id}">
      <div class="strategy-head">
        <div class="strategy-title"><b>${esc(x.name)}</b><span class="badge ${x.enabled?'':'warn'}">${x.enabled?'启用':'停用'}</span></div>
        ${admin?`<div class="row-actions"><button class="secondary edit" data-id="${x.id}">编辑</button><button class="secondary toggle" data-id="${x.id}">${x.enabled?'停用':'启用'}</button><button class="secondary test" data-id="${x.id}">测试推送</button><button class="danger del" data-id="${x.id}">删除</button></div>`:''}
      </div>
      <dl class="strategy-meta">
        <div><dt>什么时候推</dt><dd>${esc(triggerText(x))}</dd></div>
        <div><dt>推什么内容</dt><dd>${esc(x.template_name||'策略自带模板')}</dd></div>
        <div><dt>发给谁</dt><dd>${x.recipients.length?esc(x.recipients.join('、')):'全部账号'}</dd></div>
        <div><dt>推送渠道</dt><dd>${esc(tokenName(x.token_id))}</dd></div>
      </dl>
      <div class="preview-box preview-html strategy-preview">${x.preview||''}</div>
    </section>`).join(''):'<div class="empty">暂无策略</div>'}</div>
  </section>
  <section class="panel" style="margin-top:14px">
    <div class="panel-head"><h2>推送模板</h2><span class="muted">消息的标题和内容。策略选一份模板来发送；内置模板可直接编辑或重置。</span></div>
    <div class="toolbar">${admin?'<button id="addTemplate">＋ 新增模板</button>':''}<span class="muted">共 ${(ts||[]).length} 份</span></div>
    <div id="templateRows">${(ts||[]).map(t=>`<div class="template-card" data-id="${t.id}">
      <div class="template-head">
        <b>${esc(t.name)}</b>
        ${t.builtin?'<span class="badge plain-badge">内置</span>':''}
        ${admin?`<span class="row-actions"><button class="secondary t-edit" data-id="${t.id}">编辑</button><button class="secondary t-reset" data-id="${t.id}">重置</button><button class="danger t-del" data-id="${t.id}">删除</button></span>`:''}
      </div>
      <div class="preview-box preview-html template-preview-html" data-id="${t.id}">正在生成示例…</div>
    </div>`).join('')||'<div class="empty">暂无模板</div>'}</div>
  </section>`);
  // The strategy handlers.
  document.querySelector('#add')?.addEventListener('click',()=>strategyEditorModal());
  document.querySelectorAll('.edit').forEach(b=>b.onclick=()=>strategyEditorModal(xs.find(x=>x.id==b.dataset.id)));
  document.querySelectorAll('.toggle').forEach(b=>b.onclick=async()=>{
    const x=xs.find(x=>x.id==b.dataset.id);
    await action('/strategies/'+x.id,'PUT',{...x,enabled:!x.enabled});render();
  });
  document.querySelectorAll('.del').forEach(b=>b.onclick=async()=>{
    if(confirm('确定删除该策略？')){await api('/strategies/'+b.dataset.id,{method:'DELETE'});render()}
  });
  document.querySelectorAll('.test').forEach(b=>b.onclick=()=>strategyTestModal(xs.find(x=>x.id==b.dataset.id)));
  // The template handlers.
  document.querySelector('#addTemplate')?.addEventListener('click',()=>templateModal());
  document.querySelectorAll('.t-edit').forEach(b=>b.onclick=()=>templateModal((ts||[]).find(t=>t.id==b.dataset.id)));
  document.querySelectorAll('.t-reset').forEach(b=>b.onclick=async()=>{
    const t=(ts||[]).find(t=>t.id==b.dataset.id);
    if(confirm(`把模板“${t.name}”重置为内置内容？`)){
      try{await action('/push-templates/'+t.id+'/reset','POST',{});render()}catch(e){alert(e.message)}
    }
  });
  document.querySelectorAll('.t-del').forEach(b=>b.onclick=async()=>{
    try{await api('/push-templates/'+b.dataset.id,{method:'DELETE'});render()}catch(e){alert(e.message)}
  });
  // The sample render of every template card, so the page shows what the
  // words look like without opening the editor.
  for(const t of (ts||[])){
    const box=document.querySelector(`.template-preview-html[data-id="${t.id}"]`);
    if(!box)continue;
    try{
      const r=await api('/preview/strategy',{method:'POST',body:JSON.stringify({kind:t.kind,mode:'sample',title:t.title_template,content:t.body_template,item:t.item_template,format:t.format})});
      const d=r.data;
      box.innerHTML=(d.title?`<div class="preview-title">${esc(d.title)}</div>`:'')+`<div class="preview-content">${d.html||esc(d.content||'')}</div>`;
    }catch(e){
      box.textContent='示例渲染失败';
    }
  }
}

// strategyEditorModal opens the editor of a strategy. A strategy is a
// name, a trigger and a template; everything else is filled in
// The trigger of a strategy, as a visual builder. Every schedule has
// concrete controls — a time, a weekday, a day of the month — so the user
// never has to know what a cron expression is. The expression is built
// from the controls when the strategy is saved, and taken apart again when
// the strategy is edited.

// The weekdays of a cron expression, Sunday first, the way cron numbers
// them.
const WEEKDAYS=[[0,'周日'],[1,'周一'],[2,'周二'],[3,'周三'],[4,'周四'],[5,'周五'],[6,'周六']];

// humanSchedule describes the visual controls immediately, before the
// server returns the calculated next fire time.
function humanSchedule(mode,form){
  const hour=String(Number(formValue(form,'daily_hour')??formValue(form,'weekly_hour')??formValue(form,'monthly_hour')??0)).padStart(2,'0');
  const minute=String(Number(formValue(form,'daily_min')??formValue(form,'weekly_min')??formValue(form,'monthly_min')??0)).padStart(2,'0');
  if(mode==='daily')return `每天 ${hour}:${minute}`;
  if(mode==='weekly'){
    const day=Number(formValue(form,'weekly_day'));
    return `每${WEEKDAYS.find(([n])=>n===day)?.[1]||'周一'} ${hour}:${minute}`;
  }
  if(mode==='monthly')return `每月 ${Number(formValue(form,'monthly_day2'))||1} 日 ${hour}:${minute}`;
  return '';
}

// parseCronToUI takes an expression apart into the visual fields. The
// common single-value forms come back as daily, weekly or monthly; a
// schedule the controls cannot express comes back as custom, with the
// expression shown as it is.
function parseCronToUI(expr){
  const fields=(expr||'').trim().split(/\s+/);
  if(fields.length!==5)return{mode:'custom',expr:expr||''};
  const[m,h,d,mo,w]=fields;
  const single=v=>!/[*\/,-]/.test(v);
  if(mo==='*'&&d==='*'&&w==='*'&&single(m)&&single(h))return{mode:'daily',minute:m,hour:h};
  if(mo==='*'&&d==='*'&&single(m)&&single(h)&&single(w))return{mode:'weekly',minute:m,hour:h,weekday:Number(w)};
  if(mo==='*'&&w==='*'&&single(m)&&single(h)&&single(d))return{mode:'monthly',minute:m,hour:h,day:Number(d)};
  return{mode:'custom',expr:expr};
}

// hourOptions / minuteOptions are the choices of the visual time pickers.
// Two plain selects keep the value always present — a native time input
// loses half-typed values when the form is submitted too early, which is
// how a schedule could be saved with the old time.
const hourOptions=()=>Array.from({length:24},(_,h)=>`<option value="${h}" >${String(h).padStart(2,'0')} 点</option>`).join('');
const minuteOptions=()=>Array.from({length:60},(_,m)=>`<option value="${m}">${String(m).padStart(2,'0')} 分</option>`).join('');
const picked=(list,v)=>list.replace(/value="(\d+)"/g,(m,num)=>`value="${num}" ${Number(v)===Number(num)?'selected':''}`);

// formValue reads a control explicitly through its value, whether the
// caller passes the form itself or its elements collection.
function formValue(formOrElements,name){
  const collection=formOrElements?.elements||formOrElements;
  const field=collection?.namedItem?.(name)||collection?.[name]||formOrElements?.[name];
  return field&&typeof field==='object'&&'value' in field?field.value:field;
}

// uiToCron accepts either a form.elements collection or a plain object.
// Form controls must be read through .value; converting a select element
// itself to a number silently yielded 00:00 for every selected time.
function uiToCron(mode,formOrValues){
  const value=name=>formValue(formOrValues,name);
  const numeric=(name,fallback=0)=>{
    const n=Number(value(name));
    return Number.isFinite(n)?n:fallback;
  };
  if(mode==='daily')return `${numeric('daily_min')} ${numeric('daily_hour')} * * *`;
  if(mode==='weekly')return `${numeric('weekly_min')} ${numeric('weekly_hour')} * * ${numeric('weekly_day',1)}`;
  if(mode==='monthly')return `${numeric('monthly_min')} ${numeric('monthly_hour')} ${Math.min(Math.max(numeric('monthly_day2',1),1),28)} * *`;
  return String(value('cron_expr')||'');
}

// initialTriggerMode decides which visual mode an edited strategy opens
// with. A monthly strategy of an earlier release has no expression yet; it
// opens as a monthly schedule on its day, at the first check of the day.
function initialTriggerMode(x){
  if(x.type==='cycle')return'cycle';
  const cron=(x.cron_expr||'').trim();
  if(cron)return parseCronToUI(cron).mode;
  return x.type==='milestone'?'window':'monthly';
}

// strategyEditorModal opens the editor of a strategy: the trigger as a
// visual builder, the template it sends with a live sample, and the rest
// of the settings under "更多选项".
async function strategyEditorModal(x={}){
  const [templates,tokens]=await Promise.all([(await api('/push-templates')).data,(await api('/push-tokens')).data]);
  const type=x.type||'milestone';
  const fit=templates.filter(t=>t.kind===type);
  const mode0=initialTriggerMode(x);
  const parsed=parseCronToUI((x.cron_expr||'').trim());
  const offsets0=(x.offsets&&x.offsets.length)?x.offsets.join(','):'90,60,30,7,0';
  const wrap=document.createElement('div');
  wrap.className='modal-backdrop';
  const modeOptions=[
    ...(type==='milestone'?[['window','设备进入提醒窗口时推送']]:[]),
    ['daily','每天定时推送'],
    ['weekly','每周定时推送'],
    ['monthly','每月定时推送'],
    ['custom','自定义计划（Cron 表达式）'],
    ...(type==='cycle'?[['cycle','周期循环提醒']]:[]),
  ];
  wrap.innerHTML=`<section class="modal">
    <h2>${x.id?'编辑策略':'新增策略'}</h2>
    <form class="form-grid" id="strategyForm">
      <label>策略名称<input name="name" required value="${esc(x.name||'')}"></label>
      <label>什么时候推<select name="trigger_mode">${modeOptions.map(([v,l])=>`<option value="${v}" ${mode0===v?'selected':''}>${l}</option>`).join('')}</select></label>
      <div class="trigger-fields span-2">
        <label class="tf tf-window">提醒节点（设备剩余多少天时提醒，逗号分隔）<input name="offsets_text" value="${esc(offsets0)}"></label>
        <label class="tf tf-daily">推送时间（小时与分钟）<span class="time-pair"><select name="daily_hour">${picked(hourOptions(),parsed.mode==='daily'?parsed.hour:'9')}</select><select name="daily_min">${picked(minuteOptions(),parsed.mode==='daily'?parsed.minute:'0')}</select></span></label>
        <div class="tf tf-weekly tf-pair">
          <label>星期<select name="weekly_day">${WEEKDAYS.map(([v,l])=>`<option value="${v}" ${(parsed.mode==='weekly'?parsed.weekday:1)===v?'selected':''}>${l}</option>`).join('')}</select></label>
          <label>推送时间<span class="time-pair"><select name="weekly_hour">${picked(hourOptions(),parsed.mode==='weekly'?parsed.hour:'9')}</select><select name="weekly_min">${picked(minuteOptions(),parsed.mode==='weekly'?parsed.minute:'0')}</select></span></label>
        </div>
        <div class="tf tf-monthly tf-pair">
          <label>每月几号<input type="number" name="monthly_day2" min="1" max="28" value="${parsed.mode==='monthly'?parsed.day:((x.monthly_day||1))}"></label>
          <label>推送时间<span class="time-pair"><select name="monthly_hour">${picked(hourOptions(),parsed.mode==='monthly'?parsed.hour:'9')}</select><select name="monthly_min">${picked(minuteOptions(),parsed.mode==='monthly'?parsed.minute:'0')}</select></span></label>
        </div>
        <div class="tf tf-custom">
          <label>Cron 表达式（分 时 日 月 周）<input name="cron_expr" value="${esc(parsed.mode==='custom'?parsed.expr:'')}" placeholder="如：0 9,18 * * 1-5"></label>
          <p class="muted cron-hint"></p>
        </div>
        <div class="tf tf-cycle tf-pair">
          <label>到期前多少天开始提醒<input type="number" name="cycle_threshold" min="1" value="${x.cycle_threshold||30}"></label>
          <label>每隔多少天再提醒<input type="number" name="cycle_interval" min="1" value="${x.cycle_interval||7}"></label>
        </div>
        <p class="muted trigger-hint"></p>
      </div>
      <label>推什么内容<select name="template_id">${fit.map(t=>`<option value="${t.id}" ${Number(x.template_id||0)===Number(t.id)?'selected':''}>${esc(t.name)}</option>`).join('')}</select></label>
      <div class="template-preview-box"><p class="muted">正在生成示例…</p></div>
      <details class="span-2 more-options">
        <summary>更多选项（启用状态、接收人、推送渠道、汇总范围）</summary>
        <label>启用<select name="enabled"><option value="true" ${x.enabled!==false?'selected':''}>启用</option><option value="false" ${x.enabled===false?'selected':''}>停用</option></select></label>
        <label>接收账号（逗号分隔，留空＝全部账号）<input name="recipients_text" value="${esc((x.recipients||[]).join(','))}"></label>
        <label>推送渠道<select name="token_id"><option value="0">默认推送渠道</option>${tokens.map(t=>`<option value="${t.id}" ${Number(x.token_id||0)===Number(t.id)?'selected':''}>${esc(t.name)}</option>`).join('')}</select></label>
        <label class="opt-horizon">汇总范围（未来多少天以内的设备）<input type="number" name="horizon_days" min="1" value="${x.horizon_days||90}"></label>
      </details>
      <div class="error" hidden></div>
      <div class="actions">
        <button type="button" class="secondary cancel">取消</button>
        <button>保存</button>
      </div>
    </form>
  </section>`;
  document.body.append(wrap);
  const form=wrap.querySelector('form');
  const box=wrap.querySelector('.template-preview-box');

  // The fields of the chosen mode show. Update the human-readable schedule
  // synchronously from the controls first; an older describe response must
  // never overwrite the time the user just selected.
  let triggerRequest=0;
  const refreshTrigger=async()=>{
    const mode=form.elements.trigger_mode.value;
    wrap.querySelectorAll('.tf').forEach(el=>el.hidden=!el.classList.contains('tf-'+mode));
    wrap.querySelector('.opt-horizon').hidden=type!=='monthly';
    wrap.querySelector('.tf-window').hidden=!(type==='milestone'&&mode==='window');
    wrap.querySelector('.tf-cycle').hidden=type!=='cycle';
    const hint=wrap.querySelector('.trigger-hint');
    if(mode==='window'){
      hint.textContent='每天检查一次：设备剩余天数落进哪个节点，就按哪一档推送该档内的全部设备。';
      return;
    }
    if(mode==='cycle'){
      hint.textContent=`到期前 ${form.elements.cycle_threshold.value} 天内，每 ${form.elements.cycle_interval.value} 天提醒一次。`;
      return;
    }
    const expr=mode==='custom'?String(form.elements.cron_expr.value||'').trim():uiToCron(mode,form.elements);
    if(!expr){hint.textContent='请选择具体时间';return;}
    const request=++triggerRequest;
    hint.textContent=`已选计划：${mode==='custom'?expr:humanSchedule(mode,form.elements)}；正在确认下次推送时间…`;
    try{
      const r=await api('/cron-describe',{method:'POST',body:JSON.stringify({expr})});
      if(request!==triggerRequest)return;
      hint.textContent=`推送时间：${r.data.describe}，下次 ${r.data.next}`;
    }catch(e){
      if(request===triggerRequest)hint.textContent=`计划无效：${e.message}`;
    }
  };
  form.elements.trigger_mode.onchange=refreshTrigger;
  ['daily_hour','daily_min','weekly_day','weekly_hour','weekly_min','monthly_day2','monthly_hour','monthly_min','cycle_threshold','cycle_interval'].forEach(n=>{form.elements[n].onchange=refreshTrigger;});
  form.elements.cron_expr.oninput=refreshTrigger;
  refreshTrigger();

  // The preview renders the template the strategy points at, with the
  // sample data, so the user sees the message before saving.
  const refreshPreview=async()=>{
    const t=templates.find(t=>t.id===Number(form.elements.template_id.value||0));
    if(!t){box.innerHTML='<p class="muted">该类型暂无模板</p>';return;}
    box.innerHTML='<p class="muted">正在生成示例…</p>';
    try{
      const r=await api('/preview/strategy',{method:'POST',body:JSON.stringify({kind:t.kind,mode:'sample',title:t.title_template,content:t.body_template,item:t.item_template,format:t.format})});
      const d=r.data;
      box.innerHTML=(d.title?`<div class="preview-title">${esc(d.title)}</div>`:'')+`<div class="preview-content preview-html">${d.html||esc(d.content||'')}</div>`;
    }catch(e){
      box.innerHTML='<p class="muted">示例生成失败</p>';
    }
  };
  form.elements.template_id.onchange=refreshPreview;
  refreshPreview();

  form.querySelector('.cancel').onclick=()=>wrap.remove();
  form.onsubmit=async e=>{
    e.preventDefault();
    const mode=form.elements.trigger_mode.value;
    const t=templates.find(t=>t.id===Number(form.elements.template_id.value||0));
    let cronExpr='';
    let offsets=[];
    let cycleThreshold=0,cycleInterval=0;
    if(mode==='window'){
      offsets=(form.elements.offsets_text.value||'').split(',').map(s=>Number(s.trim())).filter(Number.isFinite);
      if(!offsets.length){offsets=[90,60,30,7,0];}
    }else if(mode==='daily'||mode==='weekly'||mode==='monthly'){
      cronExpr=uiToCron(mode,form.elements);
      offsets=(form.elements.offsets_text.value||'').split(',').map(s=>Number(s.trim())).filter(Number.isFinite);
    }else if(mode==='custom'){
      cronExpr=(form.elements.cron_expr.value||'').trim();
      offsets=(form.elements.offsets_text.value||'').split(',').map(s=>Number(s.trim())).filter(Number.isFinite);
    }else if(mode==='cycle'){
      cycleThreshold=Number(form.elements.cycle_threshold.value||30);
      cycleInterval=Number(form.elements.cycle_interval.value||7);
    }
    const v={
      name:form.elements.name.value,
      type:t?t.kind:type,
      enabled:form.elements.enabled.value==='true',
      template_id:Number(form.elements.template_id.value||0),
      cron_expr:cronExpr,
      offsets,
      monthly_day:Number(mode==='monthly'?form.elements.monthly_day2.value:(x.monthly_day||1)),
      horizon_days:Number(form.elements.horizon_days.value||90),
      cycle_threshold:cycleThreshold||x.cycle_threshold||30,
      cycle_interval:cycleInterval||x.cycle_interval||7,
      recipients:(form.elements.recipients_text.value||'').split(',').map(s=>s.trim()).filter(Boolean),
      token_id:Number(form.elements.token_id.value||0),
      template:'',title_template:'',item_template:'',
      format:t?t.format:'markdown',
      sort_order:x.sort_order??0,
    };
    try{
      await action('/strategies'+(x.id?'/'+x.id:''),x.id?'PUT':'POST',v);
      wrap.remove();
      // The saved schedule is echoed back, so a time that did not take is
      // seen right away instead of being discovered on the phone.
      let msg='策略已保存';
      if(v.cron_expr){
        try{
          const d=await api('/cron-describe',{method:'POST',body:JSON.stringify({expr:v.cron_expr})});
          msg='策略已保存，推送时间：'+d.data.describe;
        }catch(e){}
      }
      alert(msg);
      render();
    }catch(err){
      const box2=form.querySelector('.error');box2.textContent=err.message;box2.hidden=false;
    }
  };
}

// templateModal opens the editor of a message template. The kind travels
// with the template and is not asked again; the preview renders the words
// with the sample data as the user types, so what is written is what is
// seen.
async function templateModal(t={}){
  const formats=(await api('/message-formats')).data;
  const kind=t.kind||'milestone';
  const format=t.format||'markdown';
  const wrap=document.createElement('div');
  wrap.className='modal-backdrop';
  wrap.innerHTML=`<section class="modal modal-wide">
    <h2>${t.id?'编辑模板':'新增模板'}${t.name?` — ${esc(t.name)}`:''}</h2>
    <form class="form-grid" id="templateForm">
      ${t.id?'':'<label>模板名称<input name="name" required value="我的模板"></label>'}
      <div class="span-2">${templateEditor({kind,title:t.title_template||'',content:t.body_template||'',item:t.item_template||'',format,formats})}</div>
      <div class="span-2 error" hidden></div>
      <div class="span-2 actions">
        <button type="button" class="secondary cancel">取消</button>
        <button>保存</button>
      </div>
    </form>
  </section>`;
  document.body.append(wrap);
  wrap._formats=formats;
  wrap._format=format;
  const form=wrap.querySelector('form');
  const areas={template:form.elements.template,title_template:form.elements.title_template};
  bindMarkers(wrap,areas);
  const box=wrap.querySelector('#previewBox');
  const itemArea=form.elements.item_template;
  if(itemArea){
    const row=itemArea.closest('label');
    if(row)row.hidden=kind!=='monthly';
  }
  const refreshMarkers=()=>{
    const lists=wrap.querySelectorAll('.marker-list');
    if(lists[0])lists[0].innerHTML=markerList(kind,false).map(m=>`<button type="button" class="marker" data-key="${m.key}" title="${esc(m.label)}">${esc(m.label)}<code>{{${m.key}}}</code></button>`).join('');
    if(lists[1])lists[1].innerHTML=markerList(kind,true).map(m=>`<button type="button" class="marker" data-key="${m.key}" title="${esc(m.label)}">${esc(m.label)}<code>{{${m.key}}}</code></button>`).join('');
    bindMarkers(wrap,areas);
  };
  refreshMarkers();
  const refreshFormat=()=>{
    const f=formats.find(x=>x.value===form.elements.format.value);
    const hint=form.querySelector('.format-hint');
    if(hint)hint.textContent=f?f.hint:'';
  };
  form.elements.format.onchange=async()=>{refreshFormat();await chooseFormat(wrap,form.elements.format,box);renderSample();};
  refreshFormat();
  const presetSelect=form.elements.preset;
  if(presetSelect)presetSelect.onchange=async()=>{
    const name=presetSelect.value;
    presetSelect.value='';
    if(!name)return;
    const ok=await applyPreset(wrap,form.elements.format.value,name);
    if(!ok)alert('该格式没有这个预设模板');
    renderSample();
  };

  // The sample render follows the words as they change, so the editor
  // always shows the message the phone will show.
  let sampleTimer=null;
  const renderSample=()=>{
    clearTimeout(sampleTimer);
    sampleTimer=setTimeout(async()=>{
      try{
        const r=await api('/preview/strategy',{method:'POST',body:JSON.stringify({kind,mode:'sample',title:areas.title_template.value,content:areas.template.value,item:itemArea?.value||'',format:form.elements.format.value})});
        const d=r.data;
        box.innerHTML=(d.title?`<div class="preview-title">${esc(d.title)}</div>`:'')+`<div class="preview-content preview-html">${d.html||esc(d.content||'')}</div>`+((d.item&&kind==='monthly')?`<div class="preview-label">明细行示例</div><div class="preview-content">${esc(d.item)}</div>`:'');
      }catch(e){
        box.innerHTML='<p class="muted">示例生成失败</p>';
      }
    },400);
  };
  [areas.template,areas.title_template,itemArea].forEach(a=>{if(a)a.addEventListener('input',renderSample);});
  renderSample();

  form.querySelector('.cancel').onclick=()=>wrap.remove();
  form.onsubmit=async e=>{
    e.preventDefault();
    const v={
      name:t.id?t.name:(form.elements.name?.value||'我的模板'),
      kind,format:form.elements.format.value,
      title_template:areas.title_template.value,
      body_template:areas.template.value,
      item_template:itemArea?.value||'',
    };
    try{
      await action('/push-templates'+(t.id?'/'+t.id:''),t.id?'PUT':'POST',v);
      wrap.remove();
      render();
    }catch(err){
      const box2=form.querySelector('.error');box2.textContent=err.message;box2.hidden=false;
    }
  };
}
