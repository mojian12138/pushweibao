// The device test push opens a dialog that shows the message that will be
// sent, rendered with the data of that device. The user can change the text
// before sending it, so a test push shows what a real push would look like.

// deviceTestModal opens the test dialog for one device.
async function deviceTestModal(id){
  const device=(await api('/devices/'+id)).data;
  const formats=(await api('/message-formats')).data;
  const wrap=document.createElement('div');
  wrap.className='modal-backdrop';
  wrap.innerHTML=`<section class="modal modal-wide">
    <h2>测试推送 — ${esc(device.project||'')} ${esc(device.model||'')}</h2>
    <div class="editor">
      <section class="editor-main">
        <label class="format-row">消息格式<select name="format">${formats.map(f=>`<option value="${f.value}">${esc(f.label)}</option>`).join('')}</select></label>
        <p class="muted format-hint">${esc(formats[0]?formats[0].hint:'')}</p>
        <label>消息标题<textarea name="title" rows="2"></textarea></label>
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
            <button type="button" class="secondary" id="dvPreview">用这台设备的数据预览</button>
          </div>
          <div id="previewBox" class="preview-box muted">尚未预览</div>
        </div>
      </aside>
    </div>
    <div class="actions">
      <button type="button" class="secondary cancel">取消</button>
      <button type="button" class="secondary" id="dvSend">发送测试推送</button>
    </div>
  </section>`;
  document.body.append(wrap);
  const areas={content:wrap.querySelector('[name=content]'),title:wrap.querySelector('[name=title]')};
  const list=wrap.querySelector('.marker-list');
  list.innerHTML=markerList('milestone',false).map(m=>`<button type="button" class="marker" data-key="${m.key}" title="${esc(m.label)}">${esc(m.label)}<code>{{${m.key}}}</code></button>`).join('');
  bindMarkers(wrap,areas);
  const box=wrap.querySelector('#previewBox');
  const formatSelect=wrap.querySelector('[name=format]');
  wrap._formats=formats;
  wrap._format=formatSelect.value;
  const formatHint=()=>{
    const f=formats.find(x=>x.value===formatSelect.value);
    const hint=wrap.querySelector('.format-hint');
    if(hint)hint.textContent=f?f.hint:'';
  };
  formatSelect.onchange=async()=>{
    formatHint();
    // A message written in another format would show as its source, so the
    // message is filled with the preset of the new format.
    await chooseFormat(wrap,formatSelect,box);
    formatHint();
  };

  const payload=()=>({mode:'real',title:areas.title.value,content:areas.content.value,format:formatSelect.value});
  wrap.querySelector('#dvPreview').onclick=()=>previewTemplate('/preview/device/'+id,payload(),box).catch(()=>{});
  wrap.querySelector('.cancel').onclick=()=>wrap.remove();
  wrap.querySelector('#dvSend').onclick=async()=>{
    // The text is rendered first, so that the message that arrives is the
    // message that was shown.
    const d=await previewTemplate('/preview/device/'+id,payload(),box).catch(()=>null);
    if(!d)return;
    try{
      const r=await action('/devices/'+id+'/test-push','POST',{title:d.title,content:d.content,format:formatSelect.value});
      wrap.remove();
      alert('测试推送已发送'+(r.data&&r.data.msg_id?`，流水号 ${r.data.msg_id}`:''));
    }catch(e){
      const err=wrap.querySelector('.error');err.textContent=e.message;err.hidden=false;
    }
  };
  // The dialog starts from the preset of the format, so the first preview
  // already shows the look the message will have.
  formatHint();
  areas.content.value=formats.find(f=>f.value===formatSelect.value)?.presets?.milestone_body||'';
  areas.title.value=formats.find(f=>f.value===formatSelect.value)?.presets?.milestone_title||'';
  // The first preview fills the text areas, so the user starts from
  // something real.
  const d=await previewTemplate('/preview/device/'+id,payload(),box).catch(()=>null);
  if(d){areas.title.value=d.title||'';areas.content.value=d.content||'';}
}
