// strategyTestModal opens a test dialog for a strategy. It shows the message
// of that strategy, rendered with real data, so that the user can check it
// before it goes out to everyone.
async function strategyTestModal(x){
  const wrap=document.createElement('div');
  wrap.className='modal-backdrop';
  wrap.innerHTML=`<section class="modal modal-wide">
    <h2>测试推送 — ${esc(x.name)}</h2>
    <div class="editor">
      <section class="editor-main">
        <label>消息标题<input name="title"></label>
        <label>消息内容<textarea name="content" rows="10"></textarea></label>
        <div class="span-2 error" hidden></div>
      </section>
      <aside class="editor-side">
        <h3>可用占位符</h3>
        <p class="muted">点击即可插入到光标处</p>
        <div class="marker-list" data-target="content"></div>
        <div class="editor-preview">
          <h4>预览</h4>
          <div class="preview-toolbar">
            <button type="button" class="secondary" id="stPreview">用真实数据预览</button>
          </div>
          <div id="previewBox" class="preview-box muted">尚未预览</div>
        </div>
      </aside>
    </div>
    <div class="actions">
      <button type="button" class="secondary cancel">取消</button>
      <button type="button" id="stSend">发送测试推送</button>
    </div>
  </section>`;
  document.body.append(wrap);
  const areas={content:wrap.querySelector('[name=content]'),title:wrap.querySelector('[name=title]')};
  const list=wrap.querySelector('.marker-list');
  list.innerHTML=markerList(x.type,false).map(m=>`<button type="button" class="marker" data-key="${m.key}" title="${esc(m.label)}">${esc(m.label)}<code>{{${m.key}}}</code></button>`).join('');
  bindMarkers(wrap,areas);
  const box=wrap.querySelector('#previewBox');
  // The dialog previews the strategy in the format of the strategy, so that
  // the message shown is the message that arrives.
  const format=x.format||'html';
  const payload=()=>({kind:x.type,mode:'real',title:areas.title.value,content:areas.content.value,item:x.item_template||'',format});
  wrap.querySelector('#stPreview').onclick=()=>previewTemplate('/preview/strategy',payload(),box).catch(()=>{});
  wrap.querySelector('.cancel').onclick=()=>wrap.remove();
  wrap.querySelector('#stSend').onclick=async()=>{
    const d=await previewTemplate('/preview/strategy',payload(),box).catch(()=>null);
    if(!d)return;
    try{
      const r=await action('/strategies/'+x.id+'/test','POST',{title:d.title,content:d.content,format});
      wrap.remove();
      alert('测试推送已发送'+(r.data&&r.data.msg_id?`，流水号 ${r.data.msg_id}`:''));
    }catch(e){
      const err=wrap.querySelector('.error');err.textContent=e.message;err.hidden=false;
    }
  };
  // The strategy's own template is rendered first, so the dialog starts
  // from what the strategy really sends.
  const d=await previewTemplate('/preview/strategy',{kind:x.type,mode:'real',title:x.title_template||'',content:x.template||'',item:x.item_template||'',format},box).catch(()=>null);
  if(d){areas.title.value=d.title||'';areas.content.value=d.content||'';}
}

// The strategy editor is a modal with the settings of a strategy on the left
// and the template editor on the right. The preview is part of the editor, so
// the user sees the rendered message before saving.

// strategyEditorModal opens the editor for a strategy. It saves through the
// same API the old form used, so the saved result is the same.
async function strategyEditorModal(x={}){
  const tokens=(await api('/push-tokens')).data;
  const formats=(await api('/message-formats')).data;
  const tokenOpts=[[0,'跟随账号绑定 / 默认令牌'],...tokens.map(t=>[t.id,t.name])];
  const type=x.type||'milestone';
  const format=x.format||'html';
  const wrap=document.createElement('div');
  wrap.className='modal-backdrop';
  wrap.innerHTML=`<section class="modal modal-wide">
    <h2>${x.id?'编辑策略':'新增策略'}</h2>
    <form class="form-grid" id="strategyForm">
      <label>策略名称<input name="name" required value="${esc(x.name||'')}"></label>
      <label>类型<select name="type">
        <option value="milestone" ${type==='milestone'?'selected':''}>里程碑型</option>
        <option value="monthly" ${type==='monthly'?'selected':''}>月度汇总型</option>
        <option value="cycle" ${type==='cycle'?'selected':''}>周期循环型</option>
      </select></label>
      <label>启用<select name="enabled"><option value="true" ${x.enabled!==false?'selected':''}>启用</option><option value="false" ${x.enabled===false?'selected':''}>停用</option></select></label>
      <label>排序<input name="sort_order" type="number" value="${x.sort_order??0}"></label>
      <label>指定令牌<select name="token_id">${tokenOpts.map(([v,l])=>`<option value="${v}" ${Number(x.token_id||0)===Number(v)?'selected':''}>${esc(l)}</option>`).join('')}</select></label>
      <label>触发节点（天，逗号分隔）<input name="offsets_text" value="${esc((x.offsets||[90,60,30,7,0]).join(','))}"></label>
      <label>每月日期<input name="monthly_day" type="number" min="1" max="28" value="${x.monthly_day??1}"></label>
      <label>汇总未来天数<input name="horizon_days" type="number" min="0" value="${x.horizon_days??30}"></label>
      <label>循环阈值天数<input name="cycle_threshold" type="number" min="0" value="${x.cycle_threshold??30}"></label>
      <label>循环间隔天数<input name="cycle_interval" type="number" min="1" value="${x.cycle_interval??7}"></label>
      <label class="span-2">接收账号（逗号分隔，留空＝全部）<input name="recipients_text" value="${esc((x.recipients||[]).join(','))}"></label>
      <div class="span-2">${templateEditor({kind:type,title:x.title_template||'',content:x.template||'',item:x.item_template||'',format,formats})}</div>
      <div class="span-2 error" hidden></div>
      <div class="span-2 actions">
        <button type="button" class="secondary cancel">取消</button>
        <button type="button" class="secondary" id="testPush">预览并测试推送</button>
        <button>保存</button>
      </div>
    </form>
  </section>`;
  document.body.append(wrap);
  wrap._formats=formats;
  // The format the text belongs to, so that a declined switch can go back
  // to it.
  wrap._format=format;
  const form=wrap.querySelector('form');
  const areas={
    template:form.elements.template,
    title_template:form.elements.title_template,
  };
  bindMarkers(wrap,areas);
  const box=wrap.querySelector('#previewBox');

  // Changing the type swaps the markers on offer, because the monthly
  // summary accepts different ones. The item template only makes sense for
  // the monthly summary, so it is shown only there.
  const refreshMarkers=()=>{
    const kind=form.elements.type.value;
    const side=wrap.querySelector('.editor-side');
    const labels=side.querySelectorAll('h4');
    const lists=side.querySelectorAll('.marker-list');
    if(lists[0]){
      lists[0].innerHTML=markerList(kind,false).map(m=>`<button type="button" class="marker" data-key="${m.key}" title="${esc(m.label)}">${esc(m.label)}<code>{{${m.key}}}</code></button>`).join('');
    }
    if(lists[1]){
      lists[1].innerHTML=markerList(kind,true).map(m=>`<button type="button" class="marker" data-key="${m.key}" title="${esc(m.label)}">${esc(m.label)}<code>{{${m.key}}}</code></button>`).join('');
    }
    if(labels[0])labels[0].textContent=kind==='monthly'?'消息内容可用（汇总）':'消息内容可用';
    const item=form.elements.item_template;
    if(item){
      const row=item.closest('label');
      if(row)row.hidden=kind!=='monthly';
    }
    bindMarkers(wrap,areas);
  };
  form.elements.type.onchange=refreshMarkers;
  // The first call makes the editor match the type it was opened with.
  refreshMarkers();

  // The format decides which ready-made templates are offered, so the hint
  // under the selector follows it.
  const refreshFormat=()=>{
    const f=formats.find(x=>x.value===form.elements.format.value);
    const hint=form.querySelector('.format-hint');
    if(hint)hint.textContent=f?f.hint:'';
  };
  form.elements.format.onchange=async()=>{
    refreshFormat();
    // A text written in another format would be shown as its source — markup
    // that the phone does not draw — so the editor fills the preset of the
    // new format once the user agrees.
    await chooseFormat(wrap,form.elements.format,box);
  };
  refreshFormat();

  // A preset fills the text area that belongs to it. The templates come from
  // the server, because they belong to the format.
  const presetSelect=form.elements.preset;
  if(presetSelect)presetSelect.onchange=async()=>{
    const name=presetSelect.value;
    presetSelect.value='';
    if(!name)return;
    const ok=await applyPreset(wrap,form.elements.format.value,name);
    if(!ok)alert('该格式没有这个预设模板');
  };

  const collect=()=>{
    const v=Object.fromEntries(new FormData(form));
    v.offsets=(v.offsets_text||'').split(',').map(Number).filter(Number.isFinite);
    v.recipients=(v.recipients_text||'').split(',').map(s=>s.trim()).filter(Boolean);
    v.enabled=v.enabled==='true';
    v.token_id=Number(v.token_id||0);
    v.sort_order=Number(v.sort_order||0);
    v.monthly_day=Number(v.monthly_day||1);
    v.horizon_days=Number(v.horizon_days||30);
    v.cycle_threshold=Number(v.cycle_threshold||30);
    v.cycle_interval=Number(v.cycle_interval||1);
    v.title_template=v.title_template||'';
    v.item_template=v.item_template||'';
    v.format=v.format||'html';
    delete v.offsets_text;delete v.recipients_text;
    return v;
  };

  const payload=()=>({kind:form.elements.type.value,mode:'real',title:areas.title_template.value,content:areas.template.value,item:form.elements.item_template?.value||'',format:form.elements.format.value});

  wrap.querySelector('#previewReal').onclick=()=>previewTemplate('/preview/strategy',payload(),box).catch(()=>{});
  wrap.querySelector('#previewSample').onclick=()=>previewTemplate('/preview/strategy',{...payload(),mode:'sample'},box).catch(()=>{});
  wrap.querySelector('.cancel').onclick=()=>wrap.remove();

  // The test push sends the text that is currently in the editor, so that
  // what the user sees in the preview is what arrives.
  wrap.querySelector('#testPush').onclick=async()=>{
    const d=await previewTemplate('/preview/strategy',payload(),box).catch(()=>null);
    if(!d)return;
    try{
      const v=collect();
      const r=await action('/strategies'+(x.id?'/'+x.id+'/test':'/test'), 'POST', {
        title:d.title,
        content:d.content,
        name:v.name,
        token_id:v.token_id,
        recipients:JSON.stringify(v.recipients),
        kind:v.type,
        item:v.item_template,
        format:v.format,
      });
      alert('测试推送已发送'+(r.data&&r.data.msg_id?`，流水号 ${r.data.msg_id}`:''));
    }catch(err){
      const e=wrap.querySelector('.error');e.textContent=err.message;e.hidden=false;
    }
  };

  form.onsubmit=async e=>{
    e.preventDefault();
    const v=collect();
    try{
      await action('/strategies'+(x.id?'/'+x.id:''),x.id?'PUT':'POST',v);
      wrap.remove();
      render();
    }catch(err){
      const box2=wrap.querySelector('.error');box2.textContent=err.message;box2.hidden=false;
    }
  };
}
