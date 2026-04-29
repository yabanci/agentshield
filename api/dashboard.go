package api

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AgentShield</title>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:'Segoe UI',system-ui,sans-serif;background:#0a0c14;color:#e2e8f0;min-height:100vh}
.header{background:#111827;border-bottom:1px solid #1f2937;padding:14px 28px;display:flex;align-items:center;gap:12px}
.header h1{font-size:18px;font-weight:700;color:#fff;letter-spacing:-0.3px}
.subtitle{font-size:12px;color:#6b7280}
.badge{padding:2px 9px;border-radius:20px;font-size:11px;font-weight:600;letter-spacing:.3px;white-space:nowrap}
.green{background:#064e3b;color:#6ee7b7}.red{background:#7f1d1d;color:#fca5a5}
.yellow{background:#78350f;color:#fcd34d}.gray{background:#1f2937;color:#9ca3af}
.blue{background:#1e3a5f;color:#93c5fd}.purple{background:#3b0764;color:#d8b4fe}
.card{background:#111827;border:1px solid #1f2937;border-radius:10px;padding:18px}
.label{font-size:11px;font-weight:600;color:#6b7280;text-transform:uppercase;letter-spacing:.5px;margin-bottom:12px}
textarea{width:100%;background:#0a0c14;border:1px solid #1f2937;border-radius:7px;color:#e2e8f0;padding:10px;font-size:13px;resize:vertical;min-height:64px;outline:none;font-family:inherit}
textarea:focus{border-color:#3b82f6}
.btn{padding:8px 16px;border-radius:7px;border:none;cursor:pointer;font-size:12px;font-weight:600;transition:all .15s}
.btn-blue{background:#2563eb;color:#fff}.btn-blue:hover{background:#1d4ed8}.btn-blue:disabled{background:#1f2937;color:#4b5563;cursor:not-allowed}
.btn-red{background:#dc2626;color:#fff}.btn-red:hover{background:#b91c1c}
.btn-green{background:#059669;color:#fff}.btn-green:hover{background:#047857}
.btn-purple{background:#7c3aed;color:#fff}.btn-purple:hover{background:#6d28d9}
.btn-sm{padding:5px 11px;font-size:11px}
.resp{background:#0a0c14;border:1px solid #1f2937;border-radius:7px;padding:12px;min-height:80px;font-size:13px;line-height:1.6;white-space:pre-wrap;color:#6b7280}
.stat-grid{display:grid;grid-template-columns:1fr 1fr;gap:7px}
.stat{background:#0a0c14;border:1px solid #1f2937;border-radius:7px;padding:9px;text-align:center}
.stat-v{font-size:18px;font-weight:700;color:#f9fafb}
.stat-l{font-size:10px;color:#6b7280;margin-top:2px}
.log{font-family:monospace;font-size:11px;background:#0a0c14;border:1px solid #1f2937;border-radius:7px;padding:10px;max-height:140px;overflow-y:auto;color:#6ee7b7}
.le{margin-bottom:2px}.lw{color:#fcd34d}.le2{color:#fca5a5}.li{color:#93c5fd}
.grid{display:grid;grid-template-columns:1fr 360px;gap:16px;max-width:1200px;margin:0 auto;padding:20px}
.col{display:flex;flex-direction:column;gap:16px}
.chain{display:flex;flex-direction:column;gap:5px}
.tier{display:flex;align-items:center;gap:10px;padding:9px 12px;border-radius:7px;border:1px solid #1f2937;background:#0a0c14;transition:all .25s}
.tier.lit{border-color:#3b82f6;background:#1e3a5f22}
.tier-icon{font-size:15px;width:22px;text-align:center}
.tier-name{font-size:12px;font-weight:600;flex:1}
.tier-desc{font-size:10px;color:#6b7280}
.tab-row{display:flex;gap:0;border-bottom:1px solid #1f2937;margin-bottom:14px}
.tab{padding:8px 16px;font-size:12px;font-weight:600;cursor:pointer;color:#6b7280;border-bottom:2px solid transparent;transition:all .15s}
.tab.active{color:#3b82f6;border-bottom-color:#3b82f6}
.tab-pane{display:none}.tab-pane.active{display:block}
.row{display:flex;gap:8px;align-items:center;flex-wrap:wrap}
.demo-grid{display:grid;grid-template-columns:1fr 1fr;gap:7px}
.charts{display:grid;grid-template-columns:1fr 1fr;gap:12px;margin-top:12px}
.chart-wrap{background:#0a0c14;border:1px solid #1f2937;border-radius:7px;padding:10px}
.chart-title{font-size:10px;color:#6b7280;text-transform:uppercase;letter-spacing:.5px;margin-bottom:6px}
hr{border:none;border-top:1px solid #1f2937;margin:12px 0}
.step{padding:10px;background:#0a0c14;border:1px solid #1f2937;border-radius:7px;margin-bottom:7px;font-size:12px}
.step-thought{color:#93c5fd;margin-bottom:4px}
.step-action{color:#d8b4fe;margin-bottom:4px}
.step-obs{color:#6ee7b7;margin-bottom:4px}
.step-answer{color:#fcd34d;font-weight:600}
.step-label{font-size:10px;color:#4b5563;font-weight:700;text-transform:uppercase;margin-bottom:2px}
.mode-toggle{display:flex;gap:2px;background:#0a0c14;border:1px solid #1f2937;border-radius:7px;padding:3px}
.mode-btn{padding:5px 14px;border-radius:5px;border:none;cursor:pointer;font-size:12px;font-weight:600;background:transparent;color:#6b7280;transition:all .15s}
.mode-btn.active{background:#2563eb;color:#fff}
.sess-item{padding:8px 10px;background:#0a0c14;border:1px solid #1f2937;border-radius:6px;margin-bottom:5px;font-size:12px;cursor:pointer}
.sess-item:hover{border-color:#3b82f6}
.sess-role-user{color:#93c5fd}.sess-role-asst{color:#6ee7b7}
.chaos-log{font-family:monospace;font-size:11px;background:#0a0c14;border:1px solid #1f2937;border-radius:7px;padding:10px;max-height:200px;overflow-y:auto}
.cl-log{color:#9ca3af}.cl-prompt{color:#93c5fd}.cl-response-primary{color:#6ee7b7}
.cl-response-fallback{color:#fcd34d}.cl-response-cache{color:#a78bfa}
.cl-response-degraded{color:#fca5a5}.cl-action{color:#f97316;font-weight:600}.cl-done{color:#34d399;font-weight:700}
.tools-row{display:flex;gap:6px;flex-wrap:wrap;margin-bottom:10px}
.tool-chip{padding:3px 10px;background:#1f2937;border-radius:20px;font-size:11px;color:#9ca3af;border:1px solid #374151}
</style>
</head>
<body>
<div class="header">
  <div>
    <h1>⚡ AgentShield</h1>
    <div class="subtitle">Resilient LLM Agent — flowguard-powered</div>
  </div>
  <div style="margin-left:auto;display:flex;gap:8px;align-items:center">
    <span id="degrade-badge" class="badge red" style="display:none">🧪 degrade ON</span>
    <span id="chaos-badge" class="badge gray" style="display:none">🎭 chaos running</span>
    <span id="ollama-badge" class="badge gray">checking...</span>
  </div>
</div>

<div class="grid">
<!-- LEFT ──────────────────────────────────────────────────────────────── -->
<div class="col">

  <!-- Ask panel -->
  <div class="card">
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
      <div class="label" style="margin:0">Ask the Agent</div>
      <div class="mode-toggle">
        <button class="mode-btn active" id="mode-simple" onclick="setMode('simple')">Simple</button>
        <button class="mode-btn" id="mode-agent" onclick="setMode('agent')">🤖 Agent + Tools</button>
        <button class="mode-btn" id="mode-stream" onclick="setMode('stream')">📡 Stream</button>
      </div>
    </div>
    <div id="tools-row" class="tools-row" style="display:none"></div>
    <textarea id="prompt" placeholder="Ask anything... (Ctrl+Enter to send)"></textarea>
    <div style="margin-top:8px;display:flex;gap:8px;align-items:center">
      <button class="btn btn-blue" id="ask-btn" onclick="sendPrompt()">Send →</button>
      <span id="loading" style="display:none;font-size:11px;color:#6b7280">thinking...</span>
      <span id="session-label" style="font-size:11px;color:#4b5563;margin-left:auto"></span>
    </div>
    <div style="margin-top:10px">
      <div id="resp-meta" class="row" style="display:none;margin-bottom:6px">
        <span style="font-size:11px;color:#6b7280">via</span>
        <span id="tier-badge" class="badge"></span>
        <span id="cached-badge" class="badge yellow" style="display:none">cached</span>
        <span id="turns-badge" class="badge gray" style="display:none"></span>
        <span id="trace-badge" style="display:none;font-size:11px"></span>
      </div>
      <div class="resp" id="resp-box">Response will appear here...</div>
    </div>
  </div>

  <!-- ReAct steps (visible in agent mode) -->
  <div class="card" id="react-card" style="display:none">
    <div class="label">Reasoning Chain</div>
    <div id="steps-container"></div>
  </div>

  <!-- Tabs: Demo | Session | Charts -->
  <div class="card">
    <div class="tab-row">
      <div class="tab active" onclick="switchTab('demo')">🎭 Demo</div>
      <div class="tab" onclick="switchTab('session')">💬 Session</div>
      <div class="tab" onclick="switchTab('charts')">📊 Charts</div>
    </div>

    <!-- Demo tab -->
    <div class="tab-pane active" id="tab-demo">
      <div class="label">Transport failures</div>
      <div class="demo-grid">
        <button class="btn btn-red btn-sm" onclick="kill('primary')">💀 Kill Primary</button>
        <button class="btn btn-green btn-sm" onclick="restore('primary')">✅ Restore Primary</button>
        <button class="btn btn-red btn-sm" onclick="kill('fallback')">💀 Kill Fallback</button>
        <button class="btn btn-green btn-sm" onclick="restore('fallback')">✅ Restore Fallback</button>
      </div>
      <hr>
      <div class="label">🧪 Semantic quality degradation</div>
      <p style="font-size:11px;color:#6b7280;margin-bottom:8px">
        Primary returns <b>HTTP 200</b> but with garbage quality — repetitive, hallucinated, or incoherent responses.
        Watch the <b>semantic CB</b> open while transport CB stays closed.
      </p>
      <div class="demo-grid">
        <button class="btn btn-sm" style="background:#7c3aed;color:#fff" onclick="enableDegrade()">🧪 Enable Degrade</button>
        <button class="btn btn-green btn-sm" onclick="disableDegrade()">✅ Restore Quality</button>
      </div>
      <hr>
      <div class="label">Automated chaos scenario</div>
      <p style="font-size:11px;color:#6b7280;margin-bottom:10px">
        Scripted 4-phase scenario: baseline → kill primary → kill fallback → restore. Exercises all tiers automatically.
      </p>
      <button class="btn btn-purple" id="chaos-btn" onclick="startChaos()">▶ Run Chaos Demo</button>
      <div class="chaos-log" id="chaos-log" style="margin-top:10px;display:none"></div>
      <hr>
      <div class="label">Event log</div>
      <div class="log" id="log"></div>
    </div>

    <!-- Session tab -->
    <div class="tab-pane" id="tab-session">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:10px">
        <div class="label" style="margin:0">Current session</div>
        <button class="btn btn-sm" style="background:#1f2937;color:#9ca3af" onclick="newSession()">+ New</button>
      </div>
      <div id="session-history" style="max-height:300px;overflow-y:auto"></div>
      <div id="no-session" style="font-size:12px;color:#4b5563;text-align:center;padding:20px">
        Send a message to start a session
      </div>
    </div>

    <!-- Charts tab -->
    <div class="tab-pane" id="tab-charts">
      <div class="charts">
        <div class="chart-wrap">
          <div class="chart-title">Tier distribution</div>
          <canvas id="chart-donut" height="160"></canvas>
        </div>
        <div class="chart-wrap">
          <div class="chart-title">Requests / 3s</div>
          <canvas id="chart-line" height="160"></canvas>
        </div>
      </div>
    </div>
  </div>

</div>

<!-- RIGHT ─────────────────────────────────────────────────────────────── -->
<div class="col">

  <div class="card">
    <div class="label">Degradation Chain</div>
    <div class="chain">
      <div class="tier" id="tier-primary">
        <span class="tier-icon">🧠</span>
        <div style="flex:1">
          <div class="tier-name">Primary — llama3.2</div>
          <div class="tier-desc">Adaptive CB · retry 2x · hedge 1.5s · semantic CB</div>
        </div>
        <div style="display:flex;gap:4px;flex-direction:column;align-items:flex-end">
          <span id="cb-primary" class="badge green" title="Transport circuit breaker">transport: closed</span>
          <span id="sem-primary" class="badge green" title="Semantic quality circuit breaker">quality: healthy</span>
          <span id="quality-primary" class="badge gray" title="Rolling quality score">q: —</span>
          <span id="calib-primary" class="badge gray" title="Adaptive calibration status">calibrating…</span>
        </div>
      </div>
      <div style="padding:1px 0 1px 13px;color:#374151;font-size:10px">↓ transport or semantic circuit opens</div>
      <div class="tier" id="tier-fallback">
        <span class="tier-icon">⚡</span>
        <div style="flex:1">
          <div class="tier-name">Fallback — llama3.2:1b</div>
          <div class="tier-desc">Classic CB · semantic CB</div>
        </div>
        <div style="display:flex;gap:4px;flex-direction:column;align-items:flex-end">
          <span id="cb-fallback" class="badge green" title="Transport circuit breaker">transport: closed</span>
          <span id="sem-fallback" class="badge green" title="Semantic quality circuit breaker">quality: healthy</span>
          <span id="quality-fallback" class="badge gray" title="Rolling quality score">q: —</span>
          <span id="calib-fallback" class="badge gray" title="Adaptive calibration status">calibrating…</span>
        </div>
      </div>
      <div style="padding:1px 0 1px 13px;color:#374151;font-size:10px">↓ circuit opens</div>
      <div class="tier" id="tier-cache">
        <span class="tier-icon">💾</span>
        <div style="flex:1"><div class="tier-name">Semantic Cache</div><div class="tier-desc">nomic-embed-text · cosine sim &gt;0.92 · 10min TTL</div></div>
        <span id="cache-size-badge" class="badge gray">0 entries</span>
      </div>
      <div style="padding:1px 0 1px 13px;color:#374151;font-size:10px">↓ cache miss</div>
      <div class="tier" id="tier-degraded">
        <span class="tier-icon">🔕</span>
        <div style="flex:1"><div class="tier-name">Graceful Denial</div><div class="tier-desc">Always available</div></div>
        <span class="badge gray">last resort</span>
      </div>
    </div>
  </div>

  <div class="card">
    <div class="label">Live Stats</div>
    <div class="stat-grid">
      <div class="stat"><div class="stat-v" id="st-total">0</div><div class="stat-l">Total Requests</div></div>
      <div class="stat"><div class="stat-v" id="st-err">0%</div><div class="stat-l">Primary Error Rate</div></div>
      <div class="stat"><div class="stat-v" id="st-cache">0</div><div class="stat-l">Cache Entries</div></div>
      <div class="stat"><div class="stat-v" id="st-shed">50</div><div class="stat-l">Loadshed Limit</div></div>
      <div class="stat"><div class="stat-v" id="st-sessions">0</div><div class="stat-l">Active Sessions</div></div>
      <div class="stat"><div class="stat-v" id="st-inflight">0</div><div class="stat-l">In-Flight</div></div>
    </div>
  </div>

  <div class="card">
    <div class="label">Protection Layers</div>
    <div style="font-family:monospace;font-size:11px;color:#6b7280;line-height:1.9">
      <span style="color:#93c5fd">POST /chat</span><br>
      &nbsp;→ <span style="color:#fcd34d">Loadshed</span> (AIMD, limit: <span id="arch-shed">50</span>)<br>
      &nbsp;→ <span style="color:#fcd34d">Bulkhead</span> (interactive 20 / batch 5)<br>
      &nbsp;→ <span style="color:#fcd34d">CircuitBreaker</span> (adaptive &gt;50% err)<br>
      &nbsp;→ <span style="color:#fcd34d">Hedge</span> (duplicate at 1.5s)<br>
      &nbsp;→ <span style="color:#fcd34d">Retry</span> (2x exp backoff)<br>
      &nbsp;&nbsp;&nbsp;↓ fail → CB → fallback<br>
      &nbsp;&nbsp;&nbsp;↓ fail → semantic cache<br>
      &nbsp;&nbsp;&nbsp;↓ miss → graceful denial<br>
      <br>
      <span style="color:#93c5fd">POST /react</span><br>
      &nbsp;→ ReAct loop (max 6 iters)<br>
      &nbsp;→ Tools: calculate · get_time<br>
      &nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;search_docs · check_system<br>
      &nbsp;→ Each tool: own CB<br>
      &nbsp;→ Session history preserved<br>
      <br>
      <span style="color:#93c5fd">GET /metrics</span> → Prometheus<br>
      <span style="color:#93c5fd">GET /chat/stream</span> → SSE tokens
    </div>
  </div>

</div>
</div>

<script>
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
    t.classList.toggle('active', tabs[i]===name);
  });
  document.querySelectorAll('.tab-pane').forEach(p=>p.classList.remove('active'));
  document.getElementById('tab-'+name).classList.add('active');
}

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
    const data = await fetch('/chat',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({prompt})}).then(r=>r.json());
    document.getElementById('resp-box').textContent=data.text;
    showTier(data.tier,data.cached,0,data.trace_id);
    tierCounts[data.tier]=(tierCounts[data.tier]||0)+1; updateDonut();
    const traceLink = data.trace_id ? ' [<a href="/trace/'+data.trace_id+'" target="_blank" style="color:#93c5fd;text-decoration:none">trace↗</a>]' : '';
    document.getElementById('log').innerHTML='<div class="le '+logClass(data.tier)+'">['+new Date().toLocaleTimeString()+'] ← '+tierEmoji(data.tier)+' '+data.tier+(data.cached?' (cached)':'')+traceLink+'</div>'+document.getElementById('log').innerHTML;
  }catch(e){
    document.getElementById('resp-box').textContent='Error: '+e.message;
    log('✗ '+e.message,'le2');
  }
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

async function doStream(prompt){
  const box=document.getElementById('resp-box');
  box.textContent='';
  const url='/chat/stream?prompt='+encodeURIComponent(prompt);
  const es=new EventSource(url);
  let tier='primary';
  await new Promise(res=>{
    es.onmessage=e=>{
      const d=JSON.parse(e.data);
      if(d.done){tier=d.tier||tier;es.close();res();}
      else box.textContent+=d.token;
    };
    es.onerror=()=>{es.close();res();};
    setTimeout(()=>{es.close();res();},90000);
  });
  showTier(tier,false);
  tierCounts[tier]=(tierCounts[tier]||0)+1; updateDonut();
  log('← 📡 stream via '+tier, logClass(tier));
}

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
async function startChaos(){
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
      es.close();
      document.getElementById('chaos-btn').disabled=false;
      document.getElementById('chaos-btn').textContent='▶ Run Chaos Demo';
      refreshStatus();
    }
  };
  es.onerror=()=>{
    es.close();
    document.getElementById('chaos-btn').disabled=false;
    document.getElementById('chaos-btn').textContent='▶ Run Chaos Demo';
  };
}

// ── Manual controls ──────────────────────────────────────────────────────
async function kill(which){
  const url=which==='fallback'?'/demo/kill-fallback':'/demo/kill';
  await fetch(url,{method:'POST'});
  log('💀 '+which+' killed (transport)','lw');
  await refreshStatus();
}
async function restore(which){
  const url=which==='fallback'?'/demo/restore-fallback':'/demo/restore';
  await fetch(url,{method:'POST'});
  log('✅ '+which+' restored','');
  await refreshStatus();
}
async function enableDegrade(){
  await fetch('/demo/degrade',{method:'POST'});
  log('🧪 Degrade mode ON — primary returns low-quality responses (HTTP 200 ✓)','lw');
  await refreshStatus();
}
async function disableDegrade(){
  await fetch('/demo/restore-quality',{method:'POST'});
  log('✅ Quality restored — primary back to normal','');
  await refreshStatus();
}

// ── Status ───────────────────────────────────────────────────────────────
async function refreshStatus(){
  try{
    const s=await fetch('/status').then(r=>r.json());
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

    addLinePoint(s.total_requests);
  }catch(_){}
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
  document.getElementById('cached-badge').style.display=cached?'inline':'none';
  const turnsB=document.getElementById('turns-badge');
  if(turns){turnsB.textContent=turns+' turns';turnsB.style.display='inline';}
  else turnsB.style.display='none';
  const traceB=document.getElementById('trace-badge');
  if(traceId && traceB){
    traceB.innerHTML='<a href="/trace/'+traceId+'" target="_blank" style="color:#93c5fd;text-decoration:none">📋 trace</a>';
    traceB.style.display='inline';
  } else if(traceB) traceB.style.display='none';
}
function tierEmoji(t){return{primary:'🧠',fallback:'⚡',cache:'💾',degraded:'🔕'}[t]||'?';}
function logClass(t){return t==='degraded'?'le2':t!=='primary'?'lw':'';}
function log(msg,cls){
  const el=document.getElementById('log');
  const ts=new Date().toLocaleTimeString();
  el.innerHTML='<div class="le '+cls+'">['+ts+'] '+msg+'</div>'+el.innerHTML;
}
function esc(s){const d=document.createElement('div');d.textContent=s;return d.innerHTML;}

document.getElementById('prompt').addEventListener('keydown',e=>{
  if(e.key==='Enter'&&(e.metaKey||e.ctrlKey))sendPrompt();
});

checkOllama(); refreshStatus();
setInterval(refreshStatus,3000);
setInterval(checkOllama,15000);
</script>
</body>
</html>`
