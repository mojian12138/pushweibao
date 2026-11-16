// The template editor is the piece that makes a strategy editable: a text
// area for the message, a text area for the title, a list of the markers
// that can be inserted, and a preview that shows the rendered result.

// The markers that every kind accepts, shown when the server did not send a
// list.
const FALLBACK_MARKERS = [
  {key:'project',label:'项目名称',sample:'示例项目'},
  {key:'customer',label:'客户名称',sample:'示例客户'},
  {key:'contact',label:'联系人',sample:'张三'},
  {key:'vendor',label:'设备厂商',sample:'示例厂商'},
  {key:'model',label:'设备型号',sample:'XC-200'},
  {key:'sn',label:'设备序列号',sample:'SN000123'},
  {key:'start',label:'维保开始日期',sample:'2026-01-01'},
  {key:'end',label:'维保到期日期',sample:'2026-12-31'},
  {key:'days',label:'剩余天数',sample:'30'},
  {key:'notes',label:'备注',sample:'无'},
  {key:'owner',label:'负责人',sample:'李四'},
];
const SUMMARY_MARKERS = [
  {key:'month',label:'月份',sample:'2026-12'},
  {key:'count',label:'即将到期数量',sample:'3'},
  {key:'items',label:'即将到期明细',sample:'项目：示例项目 客户：示例客户'},
  {key:'expired',label:'已过期明细',sample:'项目：示例项目 客户：示例客户'},
  {key:'expired_count',label:'已过期数量',sample:'1'},
];

// markerList returns the markers of a kind.
function markerList(kind, forTitle){
  if(kind==='monthly'&&!forTitle)return SUMMARY_MARKERS;
  if(kind==='monthly'&&forTitle)return [...SUMMARY_MARKERS,...FALLBACK_MARKERS];
  return FALLBACK_MARKERS;
}

// insertMarker puts a marker at the end of the text area and puts the caret
// right after it.
function insertMarker(area,key){
  const token='{{'+key+'}}';
  const start=area.selectionStart??area.value.length;
  const end=area.selectionEnd??area.value.length;
  area.value=area.value.slice(0,start)+token+area.value.slice(end);
  const caret=start+token.length;
  area.focus();
  area.setSelectionRange(caret,caret);
  area.dispatchEvent(new Event('input',{bubbles:true}));
}

// markerButtons builds the clickable list of markers. The target tells which
// text area the marker is inserted into.
function markerButtons(list,target){
  return `<div class="marker-list" data-target="${target}">${list.map(m=>`<button type="button" class="marker" data-key="${m.key}" title="${esc(m.label)}">${esc(m.label)}<code>{{${m.key}}}</code></button>`).join('')}</div>`;
}

// bindMarkers wires the marker buttons to their text area.
function bindMarkers(root,areas){
  root.querySelectorAll('.marker-list').forEach(list=>{
    const area=areas[list.dataset.target];
    if(!area)return;
    list.querySelectorAll('.marker').forEach(b=>b.onclick=()=>insertMarker(area,b.dataset.key));
  });
}

// The editor holds the state of one template being edited.
//
// The format decides how the message is written — html, txt or markdown —
// and each format has its own ready-made templates, which the editor offers
// so that the user does not have to write them from scratch.
function templateEditor(opts){
  const {kind='milestone',title='',content='',item='',markers,titleMarkers,format='html',formats=[]}=opts;
  const opts2=(formats.length?formats:DEFAULT_FORMATS).map(f=>`<option value="${f.value}" ${f.value===format?'selected':''}>${esc(f.label)}</option>`).join('');
  const hint=(formats.length?formats:DEFAULT_FORMATS).find(f=>f.value===format)?.hint||'';
  return `
    <div class="editor">
      <section class="editor-main">
        <label class="format-row">消息格式<select name="format">${opts2}</select></label>
        <p class="muted format-hint">${esc(hint)}</p>
        <label>消息标题<textarea name="title_template" rows="2" placeholder="留空使用默认标题“维保到期提醒”">${esc(title)}</textarea></label>
        <label>消息内容<textarea name="template" rows="10">${esc(content)}</textarea></label>
        ${kind==='monthly'?`<label>汇总明细行格式<textarea name="item_template" rows="3" placeholder="每一台设备显示的一行">${esc(item)}</textarea></label>`:''}
        <div class="preset-row">
          <span class="muted">预设模板</span>
          <select name="preset">
            <option value="">选择后填入…</option>
            ${presetOptions(kind,formats.find(f=>f.value===format)?.labels)}
          </select>
        </div>
        <div class="span-2 error" hidden></div>
      </section>
      <aside class="editor-side">
        <h3>可用占位符</h3>
        <p class="muted">点击即可插入到光标处</p>
        <h4>消息内容可用</h4>
        ${markerButtons(markers||markerList(kind,false),'template')}
        <h4>标题另可用</h4>
        ${markerButtons(titleMarkers||markerList(kind,true),'title_template')}
        <div class="editor-preview">
          <h4>预览</h4>
          <div class="preview-toolbar">
            <button type="button" class="secondary" id="previewReal">用真实数据预览</button>
            <button type="button" class="secondary" id="previewSample">用示例数据预览</button>
          </div>
          <div id="previewBox" class="preview-box muted">尚未预览</div>
        </div>
      </aside>
    </div>`;
}

// The formats the editor offers when the server did not send the list. The
// server is the source of truth; this is only a fallback.
const DEFAULT_FORMATS=[
  {value:'html',label:'HTML',hint:'支持 HTML 文本。为空时默认使用 HTML 模板。'},
  {value:'txt',label:'纯文本',hint:'纯文本内容，不转义 HTML，换行使用 \\n。'},
  {value:'markdown',label:'Markdown',hint:'内容基于 Markdown 格式展示。'},
];

// presetOptions lists the ready-made templates for a kind. The names match
// the ones the server sends with the presets.
function presetOptions(kind,labels){
  const names=kind==='monthly'
    ?['monthly_title','monthly_body','monthly_item']
    :kind==='cycle'
      ?['cycle_title','cycle_body']
      :['milestone_title','milestone_body'];
  const fallback={milestone_title:'里程碑 · 标题',milestone_body:'里程碑 · 正文',monthly_title:'月度汇总 · 标题',monthly_body:'月度汇总 · 正文',monthly_item:'月度汇总 · 明细行',cycle_title:'周期循环 · 标题',cycle_body:'周期循环 · 正文'};
  const map={};
  (labels||[]).forEach(l=>map[l.value]=l.label);
  return names.map(n=>`<option value="${n}">${esc(map[n]||fallback[n]||n)}</option>`).join('');
}

// applyPreset fills the editor with a ready-made template of the chosen
// format. The presets are fetched from the server, because they belong to
// the format.
async function applyPreset(root,format,name){
  const formats=(root._formats||DEFAULT_FORMATS);
  const f=formats.find(x=>x.value===format);
  if(!f||!f.presets)return false;
  const text=f.presets[name];
  if(text===undefined)return false;
  const target=name.endsWith('_item')?'item_template':name.endsWith('_title')?'title_template':'template';
  // The strategy editor names its areas after the fields of the strategy,
  // while the test dialogs name them after the message, so both names are
  // tried.
  const aliases={template:['template','content'],title_template:['title_template','title'],item_template:['item_template','item']};
  let area=null;
  for(const n of aliases[target]||[target]){
    area=root.querySelector(`[name="${n}"]`);
    if(area)break;
  }
  if(!area)return false;
  area.value=text;
  area.dispatchEvent(new Event('input',{bubbles:true}));
  return true;
}

// swapFormat replaces the message of the editor with the preset of the new
// format. A text that was written in another format cannot be shown in this
// one — a plain text has no headings, an html text shown as markdown shows
// its tags — so the editor asks before it does it.
async function swapFormat(root,format){
  // The device and token dialogs have no form and no kind, so they get the
  // plain reminder presets.
  const form=root.querySelector('form');
  const kind=form?.elements?.type?.value||'milestone';
  const names=kind==='monthly'
    ?['monthly_body','monthly_title','monthly_item']
    :kind==='cycle'
      ?['cycle_body','cycle_title']
      :['milestone_body','milestone_title'];
  let done=false;
  for(const name of names){
    if(await applyPreset(root,format,name))done=true;
  }
  return done;
}

// chooseFormat is the handler of the format selector. The text is replaced
// only when the user agrees, and the selector goes back to the previous
// format when they do not, so that the format on screen is always the
// format of the text beside it.
async function chooseFormat(root,select,box,onBack){
  const next=select.value;
  const asked=`切换到${formatLabel(next)}格式？消息内容会被替换成该格式的预设模板。`;
  if(!confirm(asked)){
    select.value=root._format||DEFAULT_FORMATS[0].value;
    return next===select.value;
  }
  if(!await swapFormat(root,next)){
    alert('该格式没有可用的预设模板');
    select.value=root._format||DEFAULT_FORMATS[0].value;
    return false;
  }
  root._format=next;
  if(box){
    box.classList.add('muted');
    box.textContent='格式已切换，请重新预览';
  }
  return true;
}

// previewTemplate asks the server to render the template and shows the
// result. The server knows the real data, so the preview is trustworthy.
//
// The server also sends the message as html, so that the preview shows the
// layout of the phone and not the source of the message. A text that is not
// html is shown as it is, because the phone shows it as a text too.
async function previewTemplate(url,payload,box){
  box.classList.remove('muted');
  box.textContent='正在生成预览…';
  try{
    const r=await api(url,{method:'POST',body:JSON.stringify(payload)});
    const d=r.data;
    let html='';
    if(d.title)html+=`<div class="preview-title">${esc(d.title)}</div>`;
    const body=d.html||esc(d.content||'').replace(/\n/g,'<br>');
    html+=`<div class="preview-content preview-html" data-format="${esc(d.format||'html')}">${body}</div>`;
    // An html message is a card of its own, so the frame of the preview
    // does not box it a second time.
    box.classList.toggle('plain',d.format==='html');
    if(d.item)html+=`<div class="preview-label">明细行示例</div><pre class="preview-content">${esc(d.item)}</pre>`;
    box.innerHTML=html;
    return d;
  }catch(e){
    box.classList.add('muted');
    box.textContent='预览失败：'+e.message;
    throw e;
  }
}
