const tierColor  = {primary:'green',fallback:'yellow',cache:'purple',degraded:'red'};
const cbColor    = {closed:'green',open:'red','half-open':'yellow',killed:'red'};
let currentMode  = 'simple';
let currentSession = null;
let tierCounts   = {primary:0,fallback:0,cache:0,degraded:0};
let lastTotal    = 0;
let lineHistory  = [];

// ── Charts ──────────────────────────────────────────────────────────────
const donutData = {
  labels: ['primary','fallback','cache','degraded'],
  datasets:[{data:[0,0,0,0],backgroundColor:['#2563eb','#d97706','#7c3aed','#dc2626'],borderWidth:0}]
};
const donut = new Chart(document.getElementById('chart-donut').getContext('2d'),{
  type:'doughnut',data:donutData,
  options:{responsive:true,cutout:'65%',plugins:{legend:{position:'bottom',labels:{color:'#6b7280',font:{size:10}}}}}
});

const lineData = {labels:[],datasets:[{label:'req',data:[],borderColor:'#3b82f6',backgroundColor:'#3b82f622',tension:.4,pointRadius:2,fill:true}]};
const lineChart = new Chart(document.getElementById('chart-line').getContext('2d'),{
  type:'line',data:lineData,
  options:{responsive:true,scales:{
    x:{ticks:{color:'#4b5563',font:{size:9}},grid:{color:'#1f293744'}},
    y:{ticks:{color:'#4b5563',font:{size:9}},grid:{color:'#1f293744'},beginAtZero:true}
  },plugins:{legend:{display:false}}}
});

// Latency-per-tier histogram. Grouped bars: each tier (primary / fallback /
// cache) gets a p50 / p95 / p99 triplet. Updated on every status refresh
// so judges watching the chaos demo see latency move tier-by-tier live.
const latencyData = {
  labels: ['primary','fallback','cache'],
  datasets: [
    {label:'p50',data:[0,0,0],backgroundColor:'#22c55e'},
    {label:'p95',data:[0,0,0],backgroundColor:'#3b82f6'},
    {label:'p99',data:[0,0,0],backgroundColor:'#a855f7'},
  ],
};
const latencyChart = new Chart(document.getElementById('chart-latency').getContext('2d'),{
  type:'bar', data:latencyData,
  options:{
    responsive:true,
    scales:{
      x:{ticks:{color:'#6b7280',font:{size:10}},grid:{display:false}},
      y:{ticks:{color:'#4b5563',font:{size:9},callback:v=>v<1000?v+'ms':(v/1000).toFixed(1)+'s'},grid:{color:'#1f293744'},beginAtZero:true},
    },
    plugins:{legend:{position:'top',labels:{color:'#9ca3af',font:{size:10},boxWidth:10}}},
  },
});
function updateLatencyChart(byTier){
  if(!byTier) return;
  const tiers = ['primary','fallback','cache'];
  latencyData.datasets[0].data = tiers.map(t=>(byTier[t]?.p50_ms)||0);
  latencyData.datasets[1].data = tiers.map(t=>(byTier[t]?.p95_ms)||0);
  latencyData.datasets[2].data = tiers.map(t=>(byTier[t]?.p99_ms)||0);
  latencyChart.update('none');
}

function updateDonut(){donutData.datasets[0].data=[tierCounts.primary,tierCounts.fallback,tierCounts.cache,tierCounts.degraded];donut.update('none');}
function addLinePoint(total){
  const delta=total-lastTotal; lastTotal=total;
  const ts=new Date().toLocaleTimeString('en',{hour12:false,hour:'2-digit',minute:'2-digit',second:'2-digit'});
  lineHistory.push({ts,delta});
  if(lineHistory.length>20)lineHistory.shift();
  lineData.labels=lineHistory.map(p=>p.ts);
  lineData.datasets[0].data=lineHistory.map(p=>p.delta);
  lineChart.update('none');
}

// ── Mode toggle ─────────────────────────────────────────────────────────
function setMode(m){
  currentMode=m;
  document.querySelectorAll('.mode-btn').forEach(b=>b.classList.remove('active'));
  document.getElementById('mode-'+m).classList.add('active');
  const isAgent = m==='agent';
  document.getElementById('tools-row').style.display = isAgent ? 'flex' : 'none';
  document.getElementById('react-card').style.display = isAgent ? 'block' : 'none';
  if(isAgent) loadTools();
}

async function loadTools(){
  try{
    const tools = await fetch('/status').then(r=>r.json()).then(s=>s);
    // Tools are static — show chips
    const chips = [
      {name:'calculate',icon:'🔢'},
      {name:'get_time',icon:'🕐'},
      {name:'search_docs',icon:'📚'},
      {name:'check_system',icon:'🏥'},
    ];
    document.getElementById('tools-row').innerHTML = chips.map(t=>
      '<div class="tool-chip">'+t.icon+' '+t.name+'</div>'
    ).join('');
  }catch(e){}
}

// ── Tab switching ────────────────────────────────────────────────────────
function switchTab(name){
  document.querySelectorAll('.tab').forEach((t,i)=>{
    const tabs=['demo','session','charts'];
    const active = tabs[i]===name;
    t.classList.toggle('active', active);
    t.setAttribute('aria-selected', active ? 'true' : 'false');
  });
  document.querySelectorAll('.tab-pane').forEach(p=>p.classList.remove('active'));
  document.getElementById('tab-'+name).classList.add('active');
}
// Keyboard support for the tab list — left/right arrows cycle.
document.addEventListener('DOMContentLoaded', () => {
  const tabs = ['demo','session','charts'];
  document.querySelectorAll('[role="tablist"] .tab').forEach((tab, idx) => {
    tab.addEventListener('keydown', (ev) => {
      let next = idx;
      if (ev.key === 'ArrowRight') next = (idx + 1) % tabs.length;
      else if (ev.key === 'ArrowLeft') next = (idx + tabs.length - 1) % tabs.length;
      else return;
      ev.preventDefault();
      switchTab(tabs[next]);
      document.querySelectorAll('[role="tablist"] .tab')[next].focus();
    });
  });
});

// ── Send prompt ──────────────────────────────────────────────────────────
async function sendPrompt(){
  const prompt = document.getElementById('prompt').value.trim();
  if(!prompt) return;
  document.getElementById('ask-btn').disabled=true;
  document.getElementById('loading').style.display='inline';
  document.getElementById('resp-box').textContent='';
  document.getElementById('resp-meta').style.display='none';
  document.getElementById('steps-container').innerHTML='';

  log('→ "'+prompt.substring(0,50)+(prompt.length>50?'...:':'"'),'i');

  if(currentMode==='stream'){
    await doStream(prompt);
  } else if(currentMode==='agent'){
    await doReact(prompt);
  } else {
    await doChat(prompt);
  }

  document.getElementById('ask-btn').disabled=false;
  document.getElementById('loading').style.display='none';
  await refreshStatus();
}

async function doChat(prompt){
  try{
    const r = await fetch('/chat',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({prompt})});
    if(!r.ok){
      // Surface server-side failures instead of silently rendering nothing.
      // A 5xx from /chat used to leave resp-box empty with no log entry.
      const msg = await safeErrorMessage(r);
      document.getElementById('resp-box').textContent = 'Error '+r.status+': '+msg;
      log('✗ /chat '+r.status+' '+msg, 'le2');
      return;
    }
    const data = await r.json();
    document.getElementById('resp-box').textContent=data.text;
    showTier(data.tier,data.cached,0,data.trace_id);
    tierCounts[data.tier]=(tierCounts[data.tier]||0)+1; updateDonut();
    prependLog(data.tier, '← '+tierEmoji(data.tier)+' '+data.tier+(data.cached?' (cached)':''), data.trace_id);
  }catch(e){
    document.getElementById('resp-box').textContent='Error: '+e.message;
    log('✗ '+e.message,'le2');
  }
}

// safeErrorMessage pulls a useful string out of a non-2xx response,
// preferring the JSON {error} envelope the server emits but tolerating
// any other body without crashing the dashboard.
async function safeErrorMessage(r){
  try{
    const j = await r.json();
    return j.error || r.statusText || 'request failed';
  }catch(_){
    return r.statusText || 'request failed';
  }
}

// prependLog builds a log entry with textContent so server-controlled
// fields cannot inject HTML. Replaces the older innerHTML concatenation.
function prependLog(tier, message, traceId){
  const el = document.getElementById('log');
  const line = document.createElement('div');
  line.className = 'le ' + logClass(tier);
  line.textContent = '[' + new Date().toLocaleTimeString() + '] ' + message;
  if (traceId) {
    line.appendChild(document.createTextNode(' '));
    const a = document.createElement('a');
    a.href = '#';
    a.style.color = '#93c5fd';
    a.style.textDecoration = 'none';
    a.textContent = '[trace↗]';
    a.addEventListener('click', (ev) => { ev.preventDefault(); showTrace(traceId); });
    line.appendChild(a);
  }
  el.insertBefore(line, el.firstChild);
}

async function doReact(prompt){
  const body = {prompt};
  if(currentSession) body.session_id = currentSession;
  try{
    const data = await fetch('/react',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)}).then(r=>r.json());
    document.getElementById('resp-box').textContent = data.answer;
    showTier(data.tier, false, data.turns);
    if(currentSession!==data.session_id){
      currentSession=data.session_id;
      document.getElementById('session-label').textContent='session: '+data.session_id.substring(0,12)+'...';
    }
    renderSteps(data.steps);
    tierCounts[data.tier]=(tierCounts[data.tier]||0)+1; updateDonut();
    log('← 🤖 react ('+data.turns+' turns, '+data.tier+')', logClass(data.tier));
    refreshSessionHistory();
  }catch(e){
    document.getElementById('resp-box').textContent='Error: '+e.message;
    log('✗ '+e.message,'le2');
  }
}

// _activeStream tracks the chat EventSource so a mode switch / page hide /
// repeated Send doesn't leave the prior stream's connection + ~90s timer
// alive in the background. Both close() and clearTimeout() must run.
let _activeStream = null;

function closeActiveStream(){
  if(_activeStream){
    try { _activeStream.es.close(); } catch(_) {}
    clearTimeout(_activeStream.killTimer);
    _activeStream = null;
  }
}

async function doStream(prompt){
  closeActiveStream();
  const box=document.getElementById('resp-box');
  box.innerHTML='';
  const url='/chat/stream?prompt='+encodeURIComponent(prompt);
  const es=new EventSource(url);
  let tier='primary';
  let switched = false;
  await new Promise(res=>{
    const killTimer = setTimeout(()=>{ es.close(); res(); }, 90000);
    _activeStream = { es, killTimer };
    es.onmessage = e => {
      const d = JSON.parse(e.data);
      if(d.done){ tier = d.tier || tier; closeActiveStream(); res(); return; }
      if(d.switched){
        switched = true;
        const div = document.createElement('div');
        div.style.cssText='border-left:3px solid #f97316;background:#7c2d1222;padding:8px 12px;margin:8px 0;border-radius:5px;font-size:12px;color:#fcd34d';
        div.textContent='⚡ quality gate triggered — switched to '+(d.tier||'fallback')+(d.reason?' ('+d.reason+')':'');
        box.appendChild(div);
        tier = d.tier || 'fallback';
        return;
      }
      const tokenSpan = document.createElement('span');
      tokenSpan.textContent = d.token;
      if(switched) tokenSpan.style.background='#92400e22';
      box.appendChild(tokenSpan);
    };
    es.onerror = () => { closeActiveStream(); res(); };
  });
  showTier(tier,false);
  tierCounts[tier]=(tierCounts[tier]||0)+1; updateDonut();
  log('← 📡 stream via '+tier+(switched?' (quality-gate switched)':''), logClass(tier));
}

// Close any open EventSource when the user navigates away or hides the
// tab — otherwise the browser keeps the connection (and our 90s timer)
// alive in the background and the server-side goroutine waits out the
// full ctx timeout.
window.addEventListener('beforeunload', closeActiveStream);
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'hidden') closeActiveStream();
});

function renderSteps(steps){
  if(!steps||!steps.length) return;
  const c=document.getElementById('steps-container');
  c.innerHTML='';
  steps.forEach((s,i)=>{
    const div=document.createElement('div');
    div.className='step';
    let html='<div style="font-size:10px;color:#4b5563;margin-bottom:5px">Step '+(i+1)+'</div>';
    if(s.thought) html+='<div class="step-thought"><span class="step-label">🤔 Thought</span>'+esc(s.thought)+'</div>';
    if(s.action){
      html+='<div class="step-action"><span class="step-label">🔧 Tool</span>'+esc(s.action);
      if(s.action_input) html+=' <span style="color:#6b7280">'+esc(JSON.stringify(s.action_input))+'</span>';
      html+='</div>';
    }
    if(s.observation) html+='<div class="step-obs"><span class="step-label">📤 Result</span>'+esc(s.observation)+'</div>';
    if(s.answer) html+='<div class="step-answer"><span class="step-label">✅ Answer</span>'+esc(s.answer)+'</div>';
    div.innerHTML=html;
    c.appendChild(div);
  });
}

// ── Session history ──────────────────────────────────────────────────────
async function refreshSessionHistory(){
  if(!currentSession) return;
  try{
    const sess=await fetch('/sessions/'+currentSession).then(r=>r.json());
    const c=document.getElementById('session-history');
    document.getElementById('no-session').style.display='none';
    c.innerHTML='';
    (sess.messages||[]).forEach(m=>{
      const div=document.createElement('div');
      div.className='sess-item';
      const role=m.role==='user'?'<span class="sess-role-user">You</span>':'<span class="sess-role-asst">Agent</span>';
      div.innerHTML=role+': '+esc(m.content.substring(0,120))+(m.content.length>120?'...':'');
      c.appendChild(div);
    });
  }catch(e){}
}

function newSession(){
  currentSession=null;
  document.getElementById('session-label').textContent='';
  document.getElementById('session-history').innerHTML='';
  document.getElementById('no-session').style.display='block';
}

// ── Chaos demo ───────────────────────────────────────────────────────────
// _activeChaos prevents a double-click from opening a second EventSource
// that the server would 409 on (only one chaos scenario can run at once).
let _activeChaos = null;
function closeActiveChaos(){
  if(_activeChaos){
    try { _activeChaos.close(); } catch(_) {}
    _activeChaos = null;
  }
}
window.addEventListener('beforeunload', closeActiveChaos);
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'hidden') closeActiveChaos();
});

async function startChaos(){
  if(_activeChaos){
    log('chaos already running','i');
    return;
  }
  document.getElementById('chaos-btn').disabled=true;
  document.getElementById('chaos-btn').textContent='⏳ Running...';
  const logEl=document.getElementById('chaos-log');
  logEl.style.display='block';
  logEl.innerHTML='';

  const appendChaos=(msg,cls)=>{
    const d=document.createElement('div');
    d.className=cls;
    d.textContent=msg;
    logEl.appendChild(d);
    logEl.scrollTop=logEl.scrollHeight;
  };

  const es=new EventSource('/demo/chaos/stream');
  _activeChaos = es;
  es.onmessage=e=>{
    const ev=JSON.parse(e.data);
    const clsMap={
      log:'cl-log', prompt:'cl-prompt', done:'cl-done', action:'cl-action',
      response: ev.tier==='primary'?'cl-response-primary':
                ev.tier==='fallback'?'cl-response-fallback':
                ev.tier==='cache'?'cl-response-cache':'cl-response-degraded'
    };
    appendChaos(ev.message, clsMap[ev.type]||'cl-log');
    if(ev.type==='action'){
      if(ev.message.includes('kill_primary')) document.getElementById('cb-primary').textContent='killed';
      if(ev.message.includes('kill_fallback')) document.getElementById('cb-fallback').textContent='killed';
      if(ev.message.includes('restore_primary')){document.getElementById('cb-primary').textContent='closed';document.getElementById('cb-primary').className='badge green';}
      if(ev.message.includes('restore_fallback')){document.getElementById('cb-fallback').textContent='closed';document.getElementById('cb-fallback').className='badge green';}
    }
    if(ev.type==='done'){
      closeActiveChaos();
      document.getElementById('chaos-btn').disabled=false;
      document.getElementById('chaos-btn').textContent='▶ Run Chaos Demo';
      refreshStatus();
    }
  };
  es.onerror=()=>{
    closeActiveChaos();
    document.getElementById('chaos-btn').disabled=false;
    document.getElementById('chaos-btn').textContent='▶ Run Chaos Demo';
  };
}

// ── Manual controls ──────────────────────────────────────────────────────
// demoAction posts to a demo control endpoint and only logs success when
// the server actually applied the action. Pre-fix kill/restore/{en,dis}able
// fired-and-forgot — a 401 (auth cancelled) or any 5xx left the dashboard
// claiming the action succeeded while server state was unchanged.
async function demoAction(url, okMsg, failMsg, cls){
  let r;
  try{
    r = await authFetch(url,{method:'POST'});
  }catch(e){
    log('✗ '+failMsg+': '+e.message,'le2');
    await refreshStatus();
    return;
  }
  if(!r.ok){
    const msg = await safeErrorMessage(r);
    log('✗ '+failMsg+' '+r.status+': '+msg, 'le2');
    await refreshStatus();
    return;
  }
  log(okMsg, cls||'');
  await refreshStatus();
}

async function kill(which){
  const url = which==='fallback'?'/demo/kill-fallback':'/demo/kill';
  await demoAction(url, '💀 '+which+' killed (transport)', 'kill '+which, 'lw');
}
async function restore(which){
  const url = which==='fallback'?'/demo/restore-fallback':'/demo/restore';
  await demoAction(url, '✅ '+which+' restored', 'restore '+which, '');
}
async function enableDegrade(){
  await demoAction('/demo/degrade',
    '🧪 Degrade mode ON — primary returns low-quality responses (HTTP 200 ✓)',
    'enable degrade', 'lw');
}
async function disableDegrade(){
  await demoAction('/demo/restore-quality',
    '✅ Quality restored — primary back to normal',
    'restore quality', '');
}

// ── Status ───────────────────────────────────────────────────────────────
// _statusInFlight prevents overlapping /status fetches from stomping on
// each other. With a 3s interval + per-action calls, two requests can be
// in flight at once and the slower (older) response would win the final
// DOM write — producing a flickery "back-and-forth" UI during chaos.
let _statusInFlight = false;
async function refreshStatus(){
  if(_statusInFlight) return;
  _statusInFlight = true;
  try{
    const r = await fetch('/status');
    if(!r.ok){ _statusInFlight = false; return; }
    const s = await r.json();
    const pk=s.primary_killed?'killed':s.primary_breaker;
    const fk=s.fallback_killed?'killed':s.fallback_breaker;
    setBadge('cb-primary','transport: '+pk);
    document.getElementById('cb-primary').className='badge '+(cbColor[pk]||'gray');
    setBadge('cb-fallback','transport: '+fk);
    document.getElementById('cb-fallback').className='badge '+(cbColor[fk]||'gray');

    // Semantic CB badges
    const pSem = s.primary_semantic_cb||{};
    const fSem = s.fallback_semantic_cb||{};
    setSemBadge('sem-primary', pSem.state||'healthy', pSem.trip_reason);
    setSemBadge('sem-fallback', fSem.state||'healthy', fSem.trip_reason);

    // Quality score badges
    setQualityBadge('quality-primary', pSem.avg_quality);
    setQualityBadge('quality-fallback', fSem.avg_quality);

    // Calibration status
    updateCalibBadge('calib-primary', pSem.calibration);
    updateCalibBadge('calib-fallback', fSem.calibration);

    document.getElementById('cache-size-badge').textContent=s.cache_size+' entries';
    document.getElementById('st-total').textContent=s.total_requests;
    document.getElementById('st-err').textContent=(s.error_rate*100).toFixed(0)+'%';
    document.getElementById('st-cache').textContent=s.cache_size;
    document.getElementById('st-shed').textContent=s.loadshed_limit;
    document.getElementById('st-sessions').textContent=s.active_sessions;
    document.getElementById('st-inflight').textContent=s.loadshed_inflight;
    document.getElementById('arch-shed').textContent=s.loadshed_limit;

    document.getElementById('chaos-badge').style.display=s.chaos_running?'inline':'none';
    document.getElementById('degrade-badge').style.display=s.degrade_mode?'inline':'none';

    // Resilience Score
    const sc = s.score||{};
    const br = sc.breakdown||{};
    document.getElementById('score-total').textContent = sc.total||0;
    document.getElementById('score-grade').textContent = sc.grade||'?';
    document.getElementById('score-grade').className = 'badge '+scoreGradeClass(sc.total||0);
    document.getElementById('score-transport').textContent = br.transport_health||0;
    document.getElementById('score-quality').textContent = br.semantic_quality||0;
    document.getElementById('score-cache').textContent = br.cache_efficiency||0;
    document.getElementById('score-avail').textContent = br.availability||0;
    document.getElementById('score-latency').textContent = br.latency||0;
    document.getElementById('score-total').style.color = scoreColor(sc.total||0);

    const lat = s.latency||{};
    const p95 = lat.primary_p95_ms||0;
    document.getElementById('score-p95').textContent = p95>0 ? (p95<1000?p95+'ms':(p95/1000).toFixed(1)+'s') : '—';
    updateLatencyChart(lat.by_tier);

    // Drift detection from primary semantic CB calibration
    const driftBadge = document.getElementById('calib-primary');
    if(pSem.calibration?.drift_detected){
      driftBadge.textContent = '⚠ drift detected';
      driftBadge.className = 'badge yellow';
      driftBadge.title = 'Long-term mean ('+(Math.round(pSem.calibration.long_term_mean*100))+'%) drifted >20pp from baseline ('+(Math.round(pSem.calibration.baseline_mean*100))+'%)';
    }

    const rec = document.getElementById('score-rec');
    if(sc.recommendation){rec.textContent=sc.recommendation;rec.style.display='block';}
    else rec.style.display='none';

    // Score history sparkline
    if(window._scoreHistFetch !== false){
      fetch('/score/history').then(r=>r.json()).then(pts=>drawSparkline(pts)).catch(()=>{});
    }

    // Cost savings
    const co = s.costs||{};
    const spent = (co.spent_primary_usd||0)+(co.spent_fallback_usd||0);
    document.getElementById('cost-spent').textContent = '$'+(spent).toFixed(4);
    document.getElementById('cost-saved').textContent = '$'+(co.total_saved_usd||0).toFixed(4);
    document.getElementById('cost-pct').textContent = (co.savings_percent||0).toFixed(1)+'%';
    document.getElementById('cost-bar').style.width = Math.min(100,co.savings_percent||0)+'%';
    document.getElementById('cost-cache-hits').textContent = s.tier_counts?.cache||0;
    document.getElementById('cost-saved-cache').textContent = '$'+(co.saved_by_cache_usd||0).toFixed(4);
    document.getElementById('cost-fallback-reqs').textContent = s.tier_counts?.fallback||0;
    document.getElementById('cost-saved-fallback').textContent = '$'+(co.saved_by_fallback_usd||0).toFixed(4);

    addLinePoint(s.total_requests);
  }catch(_){}
  finally {
    _statusInFlight = false;
  }
}

function setSemBadge(id, state, reason){
  const el=document.getElementById(id);
  const labels={healthy:'quality: healthy', degraded:'quality: degraded', failing:'quality: OPEN'};
  const cls={healthy:'green', degraded:'yellow', failing:'red'};
  el.textContent=labels[state]||'quality: '+state;
  el.className='badge '+(cls[state]||'gray');
  el.title=reason||'';
}

function updateCalibBadge(id, calib){
  const el=document.getElementById(id);
  if(!calib){return;}
  if(calib.calibrated){
    const mean=Math.round(calib.baseline_mean*100);
    const std=Math.round(calib.baseline_std*100);
    el.textContent='✓ calibrated '+mean+'%±'+std+'%';
    el.className='badge green';
    el.title='Baseline: '+mean+'% ± '+std+'% | Thresholds: degraded<'+Math.round(calib.learned_degraded*100)+'%, failing<'+Math.round(calib.learned_failing*100)+'%';
  } else {
    el.textContent='calibrating '+calib.samples_collected+'/'+calib.samples_needed;
    el.className='badge gray';
  }
}

function setQualityBadge(id, avg){
  const el=document.getElementById(id);
  if(avg==null||avg===undefined){el.textContent='q: —';el.className='badge gray';return;}
  const pct=Math.round(avg*100);
  el.textContent='q: '+pct+'%';
  el.className='badge '+(pct>=70?'green':pct>=45?'yellow':'red');
}

function setBadge(id,state){
  const el=document.getElementById(id);
  el.textContent=state;
  el.className='badge '+(cbColor[state]||'gray');
}

async function checkOllama(){
  const b=document.getElementById('ollama-badge');
  try{const r=await fetch('/health');b.textContent=r.ok?'Ollama online':'Ollama offline';b.className='badge '+(r.ok?'green':'red');}
  catch(_){b.textContent='Ollama offline';b.className='badge red';}
}

// ── Helpers ──────────────────────────────────────────────────────────────
function showTier(tier,cached,turns,traceId){
  document.getElementById('resp-meta').style.display='flex';
  const tb=document.getElementById('tier-badge');
  tb.textContent=tier; tb.className='badge '+(tierColor[tier]||'gray');
  // Pulse the corresponding row in the degradation-chain card so the
  // user SEES which tier answered. CSS already defines .tier.lit;
  // round-6 frontend audit noted it was never toggled.
  document.querySelectorAll('.tier').forEach(t => t.classList.remove('lit'));
  const row = document.getElementById('tier-'+tier);
  if (row) {
    row.classList.add('lit');
    setTimeout(() => row.classList.remove('lit'), 1500);
  }
  document.getElementById('cached-badge').style.display=cached?'inline':'none';
  const turnsB=document.getElementById('turns-badge');
  if(turns){turnsB.textContent=turns+' turns';turnsB.style.display='inline';}
  else turnsB.style.display='none';
  const traceB=document.getElementById('trace-badge');
  if(traceId && traceB){
    traceB.textContent = '';
    const a = document.createElement('a');
    a.href = '#';
    a.style.color = '#93c5fd';
    a.style.textDecoration = 'none';
    a.textContent = '📋 trace';
    a.addEventListener('click', (ev) => { ev.preventDefault(); showTrace(traceId); });
    traceB.append(a);
    traceB.style.display='inline';
  } else if(traceB) traceB.style.display='none';
}
function tierEmoji(t){return{primary:'🧠',fallback:'⚡',cache:'💾',degraded:'🔕'}[t]||'?';}

// Draws the Resilience Score sparkline using HTML5 canvas (no chart lib).
function drawSparkline(points){
  const canvas = document.getElementById('score-spark');
  if(!canvas || !points || points.length===0) return;
  const ctx = canvas.getContext('2d');
  const dpr = window.devicePixelRatio||1;
  const w = canvas.clientWidth;
  const h = canvas.clientHeight;
  if(canvas.width !== w*dpr){canvas.width=w*dpr;canvas.height=h*dpr;ctx.scale(dpr,dpr);}
  ctx.clearRect(0,0,w,h);
  const min = 0, max = 100;
  ctx.beginPath();
  for(let i=0;i<points.length;i++){
    const x = (i/(points.length-1||1))*w;
    const y = h - ((points[i].total-min)/(max-min))*h;
    if(i===0) ctx.moveTo(x,y); else ctx.lineTo(x,y);
  }
  const last = points[points.length-1].total;
  ctx.strokeStyle = scoreColor(last);
  ctx.lineWidth = 2;
  ctx.stroke();
  // Fill below line for visual emphasis
  ctx.lineTo(w, h);
  ctx.lineTo(0, h);
  ctx.closePath();
  ctx.fillStyle = scoreColor(last) + '22';
  ctx.fill();
}

// Auth: prompt for token if /auth/required says so. Held in a closure
// variable; never persisted to localStorage so an XSS payload (or browser
// extension) on the same origin cannot read it back across reloads.
let _authToken = '';
async function ensureAuth(){
  try{
    const r = await fetch('/auth/required');
    const d = await r.json();
    if(d.required && !_authToken){
      const token = prompt('Auth token required for demo controls:');
      if(token){
        _authToken = token;
      }
    }
  }catch(_){}
}
function authFetch(url, opts){
  opts = opts || {};
  if(_authToken){
    opts.headers = opts.headers || {};
    opts.headers['Authorization'] = 'Bearer '+_authToken;
  }
  return fetch(url, opts);
}

// Trace viewer modal — built entirely via DOM API. Every server-controlled
// field goes through textContent / setAttribute so a prompt that induces
// HTML in trace fields cannot execute script.
async function showTrace(traceId){
  let tr;
  try { tr = await fetch('/trace/'+encodeURIComponent(traceId)).then(r=>r.json()); }
  catch(e) { alert('failed to load trace: '+e.message); return; }

  const overlay = document.createElement('div');
  overlay.style.cssText = 'position:fixed;inset:0;background:rgba(0,0,0,0.7);z-index:1000;display:flex;align-items:center;justify-content:center';
  overlay.addEventListener('click', () => overlay.remove());

  const modal = document.createElement('div');
  modal.style.cssText = 'background:#111827;border:1px solid #374151;border-radius:10px;padding:24px;max-width:700px;max-height:80vh;overflow-y:auto;width:90%';
  modal.addEventListener('click', (e) => e.stopPropagation());

  const header = document.createElement('div');
  header.style.cssText = 'display:flex;justify-content:space-between;align-items:center;margin-bottom:14px';
  const headerLeft = document.createElement('div');
  const title = document.createElement('div');
  title.style.cssText = 'font-size:14px;font-weight:600;color:#f9fafb';
  title.textContent = 'Trace ' + (tr.id || '');
  const subtitle = document.createElement('div');
  subtitle.style.cssText = 'font-size:11px;color:#6b7280';
  subtitle.textContent = (tr.total_ms != null ? tr.total_ms + 'ms · ' : '') + 'final tier: ' + (tr.final_tier || '');
  headerLeft.append(title, subtitle);
  const closeBtn = document.createElement('button');
  closeBtn.style.cssText = 'background:#1f2937;color:#fff;border:none;padding:6px 12px;border-radius:6px;cursor:pointer';
  closeBtn.textContent = '×';
  closeBtn.addEventListener('click', () => overlay.remove());
  header.append(headerLeft, closeBtn);
  modal.append(header);

  if (tr.prompt) {
    const promptRow = document.createElement('div');
    promptRow.style.cssText = 'font-size:12px;color:#9ca3af;margin-bottom:12px';
    const promptLabel = document.createTextNode('Prompt: ');
    const promptSpan = document.createElement('span');
    promptSpan.style.color = '#e2e8f0';
    promptSpan.textContent = tr.prompt;
    promptRow.append(promptLabel, promptSpan);
    modal.append(promptRow);
  }

  const tierC = {primary:'#3b82f6', fallback:'#d97706', cache:'#7c3aed', degraded:'#dc2626'};
  const outcomeC = {
    success:        '#22c55e',
    cache_hit:      '#7c3aed',
    transport_error:'#dc2626',
    semantic_failure:'#f59e0b',
    semantic_cb_open:'#f59e0b',
    killed:         '#6b7280',
    graceful_denial:'#9ca3af',
  };

  // Flame timeline strip — each step is a bar whose width is proportional to
  // step.latency_ms / total_ms. Color by outcome so success / fallback /
  // semantic_failure / graceful_denial are visually distinct at a glance.
  const steps = tr.steps || [];
  if (steps.length > 0) {
    const flameLabel = document.createElement('div');
    flameLabel.style.cssText = 'font-size:10px;color:#6b7280;margin:6px 0 4px 0;text-transform:uppercase;letter-spacing:0.5px';
    flameLabel.textContent = 'Resilience timeline';
    modal.append(flameLabel);

    const flame = document.createElement('div');
    flame.style.cssText = 'display:flex;width:100%;height:28px;border-radius:6px;overflow:hidden;background:#0a0c14;margin-bottom:6px;border:1px solid #1f2937';

    const total = Math.max(1, tr.total_ms || steps.reduce((a,s)=>a+(s.latency_ms||0), 0));
    steps.forEach((s, i) => {
      const widthPct = Math.max(2, ((s.latency_ms || 0) / total) * 100);
      const bar = document.createElement('div');
      bar.style.cssText = 'flex:0 0 '+widthPct.toFixed(2)+'%;background:'+(outcomeC[s.outcome] || tierC[s.tier] || '#6b7280')+';display:flex;align-items:center;justify-content:center;font-size:10px;color:#0a0c14;font-weight:600;cursor:default;border-right:'+(i<steps.length-1?'1px solid #0a0c14':'none');
      bar.title = (s.tier || '?') + ' · ' + (s.outcome || '?') + ' · ' + (s.latency_ms||0) + 'ms';
      // Only show the tier emoji if the bar is wide enough; otherwise just a dot.
      bar.textContent = widthPct > 8 ? tierEmoji(s.tier) : '';
      flame.append(bar);
    });
    modal.append(flame);

    // Tier scale: total duration and final outcome under the bar.
    const scale = document.createElement('div');
    scale.style.cssText = 'display:flex;justify-content:space-between;font-size:10px;color:#6b7280;margin-bottom:14px';
    const left = document.createElement('span');
    left.textContent = '0ms';
    const right = document.createElement('span');
    right.textContent = (tr.total_ms || 0) + 'ms · ended in ' + (tr.final_tier || '?');
    scale.append(left, right);
    modal.append(scale);
  }

  steps.forEach((s) => {
    const stepBox = document.createElement('div');
    stepBox.style.cssText = 'border-left:3px solid ' + (tierC[s.tier] || '#6b7280') + ';padding:10px 12px;margin-bottom:6px;background:#0a0c14;border-radius:6px';

    const topRow = document.createElement('div');
    topRow.style.cssText = 'display:flex;justify-content:space-between';
    const tierSpan = document.createElement('span');
    tierSpan.style.cssText = 'font-size:13px;font-weight:600';
    tierSpan.textContent = tierEmoji(s.tier) + ' ' + (s.tier || '');
    const latSpan = document.createElement('span');
    latSpan.style.cssText = 'font-size:11px;color:#6b7280';
    latSpan.textContent = (s.latency_ms != null ? s.latency_ms + 'ms' : '');
    topRow.append(tierSpan, latSpan);
    stepBox.append(topRow);

    const detail = document.createElement('div');
    detail.style.cssText = 'font-size:11px;color:#9ca3af;margin-top:4px';
    detail.appendChild(document.createTextNode('outcome: '));
    const outcomeB = document.createElement('b');
    outcomeB.textContent = s.outcome || '';
    detail.appendChild(outcomeB);
    if (s.transport_cb) detail.appendChild(document.createTextNode(' · transport: ' + s.transport_cb));
    if (s.semantic_cb)  detail.appendChild(document.createTextNode(' · semantic: ' + s.semantic_cb));
    if (s.quality_score != null) detail.appendChild(document.createTextNode(' · quality: ' + (s.quality_score*100).toFixed(0) + '%'));
    stepBox.append(detail);

    if (Array.isArray(s.quality_signals) && s.quality_signals.length) {
      const sig = document.createElement('div');
      sig.style.cssText = 'font-size:10px;color:#fcd34d;margin-top:3px';
      sig.textContent = 'signals: ' + s.quality_signals.join(', ');
      stepBox.append(sig);
    }
    modal.append(stepBox);
  });

  overlay.append(modal);
  document.body.append(overlay);
}
window.showTrace = showTrace;

function scoreGradeClass(n){if(n>=90)return'green';if(n>=75)return'blue';if(n>=60)return'yellow';return'red';}
function scoreColor(n){if(n>=90)return'#6ee7b7';if(n>=75)return'#93c5fd';if(n>=60)return'#fcd34d';return'#fca5a5';}
// runCompare fires POST /demo/compare and renders both sides in the panel.
// Built entirely via DOM API so server-returned text can never inject HTML
// — same threat model as showTrace().
async function runCompare(){
  const prompt = document.getElementById('prompt').value.trim();
  if(!prompt){ alert('Type a prompt first.'); return; }
  const out = document.getElementById('compare-result');
  out.textContent = '';
  out.style.display = 'block';
  const loading = document.createElement('div');
  loading.style.cssText = 'font-size:11px;color:#9ca3af;padding:8px';
  loading.textContent = 'Running both sides in parallel… (up to 90s)';
  out.appendChild(loading);

  let res;
  try {
    const r = await fetch('/demo/compare', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({prompt}),
    });
    if (!r.ok) {
      // /demo/compare has a tight 10 req/min rate limit (rounds 2+4).
      // Surface 429 distinctly so the user knows to slow down.
      const msg = await safeErrorMessage(r);
      out.textContent = '';
      const err = document.createElement('div');
      err.style.cssText = 'color:#fca5a5;font-size:11px;padding:8px';
      err.textContent = (r.status === 429 ? 'Rate-limited (10/min for compare). Wait a few seconds and retry.'
                                          : 'Compare failed: '+r.status+' '+msg);
      out.appendChild(err);
      return;
    }
    res = await r.json();
  } catch(e) {
    out.textContent = '';
    const err = document.createElement('div');
    err.style.cssText = 'color:#fca5a5;font-size:11px;padding:8px';
    err.textContent = 'Compare failed: ' + e.message;
    out.appendChild(err);
    return;
  }

  out.textContent = '';
  const grid = document.createElement('div');
  grid.style.cssText = 'display:grid;grid-template-columns:1fr 1fr;gap:8px';

  grid.appendChild(buildCompareSide('🛡️ Shielded', res.shielded, '#22c55e'));
  grid.appendChild(buildCompareSide('🪤 Raw', res.raw, '#ef4444'));
  out.appendChild(grid);

  const foot = document.createElement('div');
  foot.style.cssText = 'font-size:10px;color:#6b7280;margin-top:6px;text-align:right';
  foot.textContent = 'Total ' + (res.duration_ms||0) + 'ms (both ran concurrently)';
  out.appendChild(foot);
}

function buildCompareSide(title, side, color){
  const box = document.createElement('div');
  box.style.cssText = 'border:1px solid '+color+'55;border-radius:6px;padding:10px;background:#0a0c14';

  const header = document.createElement('div');
  header.style.cssText = 'display:flex;justify-content:space-between;align-items:center;margin-bottom:6px';
  const titleEl = document.createElement('span');
  titleEl.style.cssText = 'font-size:12px;font-weight:600;color:'+color;
  titleEl.textContent = title;
  const lat = document.createElement('span');
  lat.style.cssText = 'font-size:10px;color:#9ca3af';
  lat.textContent = (side.latency_ms || 0) + 'ms';
  header.append(titleEl, lat);
  box.append(header);

  const meta = document.createElement('div');
  meta.style.cssText = 'display:flex;gap:6px;font-size:10px;color:#6b7280;margin-bottom:6px;flex-wrap:wrap';
  if (side.tier) {
    const tierTag = document.createElement('span');
    tierTag.style.cssText = 'background:#1f2937;color:#9ca3af;padding:2px 6px;border-radius:4px';
    tierTag.textContent = 'tier: ' + side.tier;
    meta.appendChild(tierTag);
  }
  if (side.quality_score != null) {
    const qTag = document.createElement('span');
    const q = side.quality_score;
    const qc = q >= 0.7 ? '#22c55e' : (q >= 0.45 ? '#f59e0b' : '#ef4444');
    qTag.style.cssText = 'background:'+qc+'22;color:'+qc+';padding:2px 6px;border-radius:4px';
    qTag.textContent = 'quality: ' + Math.round(q*100) + '%';
    meta.appendChild(qTag);
  }
  if (side.cached) {
    const c = document.createElement('span');
    c.style.cssText = 'background:#7c3aed22;color:#a78bfa;padding:2px 6px;border-radius:4px';
    c.textContent = 'cached';
    meta.appendChild(c);
  }
  if (meta.children.length) box.append(meta);

  const text = document.createElement('div');
  text.style.cssText = 'font-size:11px;color:#e2e8f0;background:#111827;padding:8px;border-radius:4px;max-height:180px;overflow-y:auto;white-space:pre-wrap;font-family:inherit';
  text.textContent = side.error ? ('ERROR: ' + side.error) : (side.text || '(empty)');
  box.append(text);

  return box;
}
window.runCompare = runCompare;

function logClass(t){return t==='degraded'?'le2':t!=='primary'?'lw':'';}
// log appends a text-only log line. msg is treated as plain text — never HTML.
function log(msg,cls){
  const el=document.getElementById('log');
  const ts=new Date().toLocaleTimeString();
  const line = document.createElement('div');
  line.className = 'le ' + (cls || '');
  line.textContent = '[' + ts + '] ' + msg;
  el.insertBefore(line, el.firstChild);
}
function esc(s){const d=document.createElement('div');d.textContent=s;return d.innerHTML;}

document.getElementById('prompt').addEventListener('keydown',e=>{
  if(e.key==='Enter'&&(e.metaKey||e.ctrlKey))sendPrompt();
});

ensureAuth().then(()=>{
  checkOllama();
  refreshStatus();
});
setInterval(refreshStatus,3000);
setInterval(checkOllama,15000);
