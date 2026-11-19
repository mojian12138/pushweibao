// The log page shows what the system did. It reads the system log, filters
// it and pages through it.

// logState keeps the filter between renders, so that changing the filter
// does not reset the page.
const logState={level:'',category:'',search:'',actor:'',from:'',to:'',order:'desc',limit:100,offset:0};

async function logs(){
  const el=document.querySelector('.page')||app;
  el.innerHTML='<div class="loading">正在加载日志…</div>';
  let meta={levels:[],categories:[],actors:[]};
  try{
    const m=await api('/logs/meta');
    if(m.data)meta=m.data;
  }catch{}
  let data={total:0,items:[]};
  try{
    const p=new URLSearchParams();
    for(const k of ['level','category','search','actor','from','to','order'])if(logState[k])p.set(k,logState[k]);
    p.set('limit',logState.limit);p.set('offset',logState.offset);
    const r=await api('/logs?'+p.toString());
    if(r.data)data=r.data;
  }catch(e){
    el.innerHTML=`<div class="error">${esc(e.message)} <button class="secondary" onclick="render()">重试</button></div>`;
    return;
  }
  const page=Math.floor(logState.offset/logState.limit)+1;
  const pages=Math.max(1,Math.ceil((data.total||0)/logState.limit));
  shell(`
    <h1>系统日志</h1>
    <div class="toolbar">
      <input id="logSearch" placeholder="搜索操作、消息或详情" value="${esc(logState.search)}">
      <select id="logLevel"><option value="">全部级别</option>${(meta.levels||[]).map(l=>`<option value="${l}" ${logState.level===l?'selected':''}>${levelLabel(l)}</option>`).join('')}</select>
      <select id="logCategory"><option value="">全部分类</option>${(meta.categories||[]).map(c=>`<option value="${c}" ${logState.category===c?'selected':''}>${categoryLabel(c)}</option>`).join('')}</select>
      <select id="logActor"><option value="">全部账号</option>${(meta.actors||[]).map(a=>`<option value="${esc(a)}" ${logState.actor===a?'selected':''}>${esc(a)}</option>`).join('')}</select>
      <label>从 <input id="logFrom" type="date" value="${esc(logState.from)}"></label>
      <label>至 <input id="logTo" type="date" value="${esc(logState.to)}"></label>
      <select id="logOrder"><option value="desc" ${logState.order==='desc'?'selected':''}>最新在前</option><option value="asc" ${logState.order==='asc'?'selected':''}>最早在前</option></select>
      <button id="logFilter">筛选</button>
      <button class="secondary" id="logReset">重置</button>
    </div>
    <div class="muted" style="margin-bottom:10px">共 ${data.total||0} 条，第 ${page} / ${pages} 页</div>
	    <div id="rows">${table(['时间','级别','分类','操作','消息','详情','账号','IP'],(data.items||[]).map(l=>[esc(l.created_at),levelBadge(l.level),esc(categoryLabel(l.category)),esc(l.action),esc(l.message),`<details><summary>查看</summary><pre class="log-detail">${esc(l.detail||'')}</pre></details>`,esc(l.actor),esc(l.ip)]))}</div>
    <div class="toolbar" style="margin-top:12px">
      <button class="secondary" id="logPrev" ${page<=1?'disabled':''}>上一页</button>
      <button class="secondary" id="logNext" ${page>=pages?'disabled':''}>下一页</button>
    </div>`);
  const apply=()=>{
    logState.search=document.querySelector('#logSearch').value.trim();
    logState.level=document.querySelector('#logLevel').value;
    logState.category=document.querySelector('#logCategory').value;
    logState.actor=document.querySelector('#logActor').value;
    logState.from=document.querySelector('#logFrom').value;
    logState.to=document.querySelector('#logTo').value;
    logState.order=document.querySelector('#logOrder').value;
    logState.offset=0;
    render();
  };
  document.querySelector('#logFilter').onclick=apply;
  document.querySelector('#logSearch').onkeydown=e=>{if(e.key==='Enter')apply()};
  document.querySelector('#logReset').onclick=()=>{Object.assign(logState,{level:'',category:'',search:'',actor:'',from:'',to:'',offset:0});render()};
  document.querySelector('#logPrev').onclick=()=>{logState.offset=Math.max(0,logState.offset-logState.limit);render()};
  document.querySelector('#logNext').onclick=()=>{logState.offset+=logState.limit;render()};
}

// levelLabel gives a level a readable name.
function levelLabel(l){return {debug:'调试',info:'信息',warn:'警告',error:'错误'}[l]||l}
// categoryLabel gives a category a readable name.
function categoryLabel(c){return {system:'系统',auth:'登录',push:'推送',schedule:'调度',data:'数据',strategy:'策略'}[c]||c}
// levelBadge shows a level as a coloured badge.
function levelBadge(l){
  const cls={debug:'',info:'',warn:'warn',error:'bad'}[l]||'';
  return `<span class="badge ${cls}">${levelLabel(l)}</span>`;
}
