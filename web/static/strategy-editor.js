// strategyTestModal opens a test dialog for a strategy. It shows the
// message of that strategy, rendered from the real devices of the window,
// so that the user can check it before it goes out to everyone.
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
  const format=x.format||'markdown';
  // The preview renders the window the strategy covers: every device
  // inside the horizon of the trigger, grouped and sorted the way the
  // message will lay them out. The offsets and the horizon travel with the
  // request, so the preview shows what this strategy sends.
  const payload=()=>({kind:x.type,mode:'real',title:areas.title.value,content:areas.content.value,item:x.item_template||'',format,
    offsets:x.type==='milestone'?(x.offsets||[]):[], window_days:x.type==='milestone'?Math.max(...(x.offsets||[90])):0,
    horizon_days:x.type==='monthly'?(x.horizon_days||90):0});
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
  const d=await previewTemplate('/preview/strategy',{kind:x.type,mode:'real',title:x.title_template||'',content:x.template||'',item:x.item_template||'',format,
    offsets:x.type==='milestone'?(x.offsets||[]):[], window_days:x.type==='milestone'?Math.max(...(x.offsets||[90])):0,
    horizon_days:x.type==='monthly'?(x.horizon_days||90):0},box).catch(()=>null);
  if(d){areas.title.value=d.title||'';areas.content.value=d.content||'';}
}

// The strategy editor is a modal with the trigger of the strategy on the
// left and the template editor on the right. The trigger is picked from
// the visual presets of its type, or written as a cron expression.

