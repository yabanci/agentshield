package api

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AgentShield — Resilient LLM Demo</title>
<script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>
<style>
  *{box-sizing:border-box;margin:0;padding:0}
  body{font-family:'Segoe UI',system-ui,sans-serif;background:#0f1117;color:#e2e8f0;min-height:100vh}
  .header{background:#1a1d2e;border-bottom:1px solid #2d3748;padding:16px 32px;display:flex;align-items:center;gap:12px}
  .header h1{font-size:20px;font-weight:700;color:#fff}
  .subtitle{font-size:13px;color:#718096}
  .badge{padding:3px 10px;border-radius:20px;font-size:11px;font-weight:600;letter-spacing:.5px}
  .bg{background:#1a1d2e;border:1px solid #2d3748;border-radius:12px;padding:20px}
  .label{font-size:13px;font-weight:600;color:#a0aec0;text-transform:uppercase;letter-spacing:.5px;margin-bottom:14px}
  .green{background:#22543d;color:#68d391}
  .red{background:#742a2a;color:#fc8181}
  .yellow{background:#744210;color:#f6e05e}
  .gray{background:#2d3748;color:#a0aec0}
  .blue{background:#1a365d;color:#63b3ed}
  .chain{display:flex;flex-direction:column;gap:6px}
  .tier{display:flex;align-items:center;gap:12px;padding:10px 14px;border-radius:8px;border:1px solid #2d3748;background:#0f1117;transition:border-color .3s,background .3s}
  .tier.active{border-color:#4299e1;background:#1a365d22}
  .tier-icon{font-size:16px;width:24px;text-align:center}
  .tier-name{font-size:13px;font-weight:600;flex:1}
  .tier-desc{font-size:11px;color:#718096}
  textarea{width:100%;background:#0f1117;border:1px solid #2d3748;border-radius:8px;color:#e2e8f0;padding:12px;font-size:14px;resize:vertical;min-height:72px;outline:none;font-family:inherit}
  textarea:focus{border-color:#4299e1}
  .btn{padding:9px 18px;border-radius:8px;border:none;cursor:pointer;font-size:13px;font-weight:600;transition:all .2s}
  .btn-blue{background:#3182ce;color:#fff}.btn-blue:hover{background:#2b6cb0}.btn-blue:disabled{background:#2d3748;color:#718096;cursor:not-allowed}
  .btn-red{background:#c53030;color:#fff}.btn-red:hover{background:#9b2c2c}
  .btn-green{background:#276749;color:#fff}.btn-green:hover{background:#22543d}
  .btn-sm{padding:6px 12px;font-size:12px}
  .resp{background:#0f1117;border:1px solid #2d3748;border-radius:8px;padding:14px;min-height:90px;font-size:14px;line-height:1.6;white-space:pre-wrap;color:#718096}
  .stat-grid{display:grid;grid-template-columns:1fr 1fr;gap:8px}
  .stat{background:#0f1117;border:1px solid #2d3748;border-radius:8px;padding:10px;text-align:center}
  .stat-v{font-size:20px;font-weight:700}
  .stat-l{font-size:10px;color:#718096;margin-top:3px}
  .log{font-family:monospace;font-size:11px;background:#0f1117;border:1px solid #2d3748;border-radius:8px;padding:10px;max-height:150px;overflow-y:auto;color:#68d391}
  .log-e{margin-bottom:3px}
  .w{color:#f6e05e}.e{color:#fc8181}.i{color:#63b3ed}
  .grid2{display:grid;grid-template-columns:1fr 380px;gap:20px;max-width:1140px;margin:0 auto;padding:24px}
  .col{display:flex;flex-direction:column;gap:20px}
  .gap{margin-top:10px}
  .row{display:flex;gap:8px;align-items:center;flex-wrap:wrap}
  .demo-grid{display:grid;grid-template-columns:1fr 1fr;gap:8px}
  .charts{display:grid;grid-template-columns:1fr 1fr;gap:16px;margin-top:16px}
  .chart-wrap{background:#0f1117;border:1px solid #2d3748;border-radius:8px;padding:12px}
  .chart-title{font-size:11px;color:#718096;text-transform:uppercase;letter-spacing:.5px;margin-bottom:8px}
  hr{border:none;border-top:1px solid #2d3748;margin:14px 0}
  .stream-toggle{display:flex;gap:8px;align-items:center;margin-top:8px}
  .stream-toggle label{font-size:12px;color:#718096}
  input[type=checkbox]{accent-color:#3182ce}
</style>
</head>
<body>
<div class="header">
  <div>
    <h1>⚡ AgentShield</h1>
    <div class="subtitle">Resilient LLM Agent — flowguard-powered degradation chain</div>
  </div>
  <div style="margin-left:auto;display:flex;gap:8px;align-items:center">
    <span id="ollama-badge" class="badge gray">checking...</span>
  </div>
</div>

<div class="grid2">
  <!-- LEFT -->
  <div class="col">

    <div class="bg">
      <div class="label">Ask the Agent</div>
      <textarea id="prompt" placeholder="Ask anything... (Ctrl+Enter to send)"></textarea>
      <div class="stream-toggle">
        <input type="checkbox" id="stream-mode">
        <label for="stream-mode">Streaming mode (SSE)</label>
      </div>
      <div class="gap row">
        <button class="btn btn-blue" id="ask-btn" onclick="sendPrompt()">Send →</button>
        <span id="loading" style="display:none;color:#718096;font-size:12px">thinking...</span>
      </div>
      <div class="gap">
        <div id="resp-meta" class="row gap" style="display:none;margin-bottom:8px">
          <span style="font-size:12px;color:#718096">answered by</span>
          <span id="tier-badge" class="badge"></span>
          <span id="cached-badge" class="badge yellow" style="display:none">cached</span>
        </div>
        <div class="resp" id="resp-box">Response will appear here...</div>
      </div>
    </div>

    <div class="bg">
      <div class="label">Demo Controls</div>
      <p style="font-size:12px;color:#718096;margin-bottom:10px">Inject failures to watch the degradation chain live.</p>
      <div class="demo-grid">
        <button class="btn btn-red btn-sm" onclick="kill('primary')">💀 Kill Primary</button>
        <button class="btn btn-green btn-sm" onclick="restore('primary')">✅ Restore Primary</button>
        <button class="btn btn-red btn-sm" onclick="kill('fallback')">💀 Kill Fallback</button>
        <button class="btn btn-green btn-sm" onclick="restore('fallback')">✅ Restore Fallback</button>
      </div>
      <hr>
      <div class="label">Event Log</div>
      <div class="log" id="log"></div>
    </div>

    <div class="bg">
      <div class="label">Request Distribution</div>
      <div class="charts">
        <div class="chart-wrap">
          <div class="chart-title">Tier distribution</div>
          <canvas id="chart-donut" height="160"></canvas>
        </div>
        <div class="chart-wrap">
          <div class="chart-title">Requests over time</div>
          <canvas id="chart-line" height="160"></canvas>
        </div>
      </div>
    </div>

  </div>

  <!-- RIGHT -->
  <div class="col">

    <div class="bg">
      <div class="label">Degradation Chain</div>
      <div class="chain">
        <div class="tier" id="tier-primary">
          <span class="tier-icon">🧠</span>
          <div style="flex:1"><div class="tier-name">Primary — llama3.2</div><div class="tier-desc">CB (adaptive) + retry + hedge (1.5s)</div></div>
          <span id="cb-primary" class="badge green">closed</span>
        </div>
        <div style="padding:2px 0 2px 14px;color:#4a5568;font-size:11px">↓ circuit opens or timeout</div>
        <div class="tier" id="tier-fallback">
          <span class="tier-icon">⚡</span>
          <div style="flex:1"><div class="tier-name">Fallback — llama3.2:1b</div><div class="tier-desc">CB (classic, 3 failures)</div></div>
          <span id="cb-fallback" class="badge green">closed</span>
        </div>
        <div style="padding:2px 0 2px 14px;color:#4a5568;font-size:11px">↓ circuit opens</div>
        <div class="tier" id="tier-cache">
          <span class="tier-icon">💾</span>
          <div style="flex:1"><div class="tier-name">Semantic Cache</div><div class="tier-desc">Cosine similarity · nomic-embed-text · 10min TTL</div></div>
          <span id="cache-size-badge" class="badge gray">0 entries</span>
        </div>
        <div style="padding:2px 0 2px 14px;color:#4a5568;font-size:11px">↓ cache miss</div>
        <div class="tier" id="tier-degraded">
          <span class="tier-icon">🔕</span>
          <div style="flex:1"><div class="tier-name">Graceful Denial</div><div class="tier-desc">Always available</div></div>
          <span class="badge gray">last resort</span>
        </div>
      </div>
    </div>

    <div class="bg">
      <div class="label">Live Stats</div>
      <div class="stat-grid">
        <div class="stat"><div class="stat-v" id="st-total">0</div><div class="stat-l">Total Requests</div></div>
        <div class="stat"><div class="stat-v" id="st-err">0%</div><div class="stat-l">Primary Error Rate</div></div>
        <div class="stat"><div class="stat-v" id="st-cache">0</div><div class="stat-l">Cache Entries</div></div>
        <div class="stat"><div class="stat-v" id="st-shed">50</div><div class="stat-l">Loadshed Limit</div></div>
        <div class="stat"><div class="stat-v" id="st-inflight">0</div><div class="stat-l">In-Flight</div></div>
        <div class="stat"><div class="stat-v" id="st-bh">0/0</div><div class="stat-l">Bulkheads (int/batch)</div></div>
      </div>
    </div>

    <div class="bg">
      <div class="label">Architecture</div>
      <div style="font-family:monospace;font-size:11px;color:#a0aec0;line-height:1.8">
        POST /chat<br>
        &nbsp;&nbsp;→ Loadshed (AIMD)<br>
        &nbsp;&nbsp;→ Bulkhead (interactive | batch)<br>
        &nbsp;&nbsp;→ CircuitBreaker (adaptive)<br>
        &nbsp;&nbsp;→ Hedge (1.5s delay)<br>
        &nbsp;&nbsp;→ Retry (2x exp backoff)<br>
        &nbsp;&nbsp;→ Ollama primary<br>
        &nbsp;&nbsp;&nbsp;&nbsp;↓ fail → CircuitBreaker → Ollama fallback<br>
        &nbsp;&nbsp;&nbsp;&nbsp;↓ fail → Semantic cache (cosine sim)<br>
        &nbsp;&nbsp;&nbsp;&nbsp;↓ miss → Graceful denial<br>
        <br>
        GET /chat/stream → SSE token stream<br>
        GET /metrics&nbsp;&nbsp;&nbsp;&nbsp;→ Prometheus
      </div>
    </div>

  </div>
</div>

<script>
const tierColor = {primary:'green',fallback:'yellow',cache:'yellow',degraded:'red'};
const cbColor   = {closed:'green',open:'red','half-open':'yellow',killed:'red'};

// ─── Charts ────────────────────────────────────────────────────────────────
const chartOpts = {
  responsive:true,
  plugins:{legend:{labels:{color:'#a0aec0',font:{size:11}}}},
};

const donutCtx  = document.getElementById('chart-donut').getContext('2d');
const donutData = {labels:['primary','fallback','cache','degraded'],datasets:[{data:[0,0,0,0],backgroundColor:['#3182ce','#d69e2e','#2f855a','#c53030'],borderWidth:0}]};
const donut = new Chart(donutCtx,{type:'doughnut',data:donutData,options:{...chartOpts,cutout:'65%',plugins:{legend:{position:'bottom',labels:{color:'#a0aec0',font:{size:10}}}}}});

const lineCtx  = document.getElementById('chart-line').getContext('2d');
const lineData = {labels:[],datasets:[{label:'req/10s',data:[],borderColor:'#4299e1',backgroundColor:'#4299e122',tension:.4,pointRadius:2,fill:true}]};
const line = new Chart(lineCtx,{type:'line',data:lineData,options:{...chartOpts,scales:{x:{ticks:{color:'#718096',font:{size:9}},grid:{color:'#2d374844'}},y:{ticks:{color:'#718096',font:{size:9}},grid:{color:'#2d374844'},beginAtZero:true}}}});

let tierCounts = {primary:0,fallback:0,cache:0,degraded:0};
let lastTotal  = 0;
let lineHistory = [];

function addPoint(total) {
  const delta = total - lastTotal;
  lastTotal = total;
  const ts = new Date().toLocaleTimeString('en',{hour12:false,hour:'2-digit',minute:'2-digit',second:'2-digit'});
  lineHistory.push({ts,delta});
  if(lineHistory.length > 20) lineHistory.shift();
  lineData.labels   = lineHistory.map(p=>p.ts);
  lineData.datasets[0].data = lineHistory.map(p=>p.delta);
  line.update('none');
}

function updateDonut() {
  donutData.datasets[0].data = [tierCounts.primary,tierCounts.fallback,tierCounts.cache,tierCounts.degraded];
  donut.update('none');
}

// ─── Log ───────────────────────────────────────────────────────────────────
function log(msg,type='') {
  const el=document.getElementById('log');
  const ts=new Date().toLocaleTimeString();
  el.innerHTML='<div class="log-e '+type+'">['+ts+'] '+msg+'</div>'+el.innerHTML;
}

// ─── Status polling ────────────────────────────────────────────────────────
async function refreshStatus() {
  try {
    const s = await fetch('/status').then(r=>r.json());
    const pk = s.primary_killed ? 'killed' : s.primary_breaker;
    const fk = s.fallback_killed? 'killed' : s.fallback_breaker;

    setBadge('cb-primary',  pk);
    setBadge('cb-fallback', fk);
    document.getElementById('cache-size-badge').textContent = s.cache_size+' entries';

    document.getElementById('st-total').textContent    = s.total_requests;
    document.getElementById('st-err').textContent      = (s.error_rate*100).toFixed(0)+'%';
    document.getElementById('st-cache').textContent    = s.cache_size;
    document.getElementById('st-shed').textContent     = s.loadshed_limit;
    document.getElementById('st-inflight').textContent = s.loadshed_inflight;
    document.getElementById('st-bh').textContent       = s.interactive_busy+'/'+s.batch_busy;

    addPoint(s.total_requests);
  } catch(_) {}
}

function setBadge(id, state) {
  const el = document.getElementById(id);
  el.textContent = state;
  el.className   = 'badge '+(cbColor[state]||'gray');
}

async function checkOllama() {
  const b = document.getElementById('ollama-badge');
  try {
    const r = await fetch('/health');
    b.textContent = r.ok ? 'Ollama online' : 'Ollama offline';
    b.className   = 'badge '+(r.ok ? 'green' : 'red');
  } catch(_) { b.textContent='Ollama offline'; b.className='badge red'; }
}

// ─── Send prompt ───────────────────────────────────────────────────────────
async function sendPrompt() {
  const prompt = document.getElementById('prompt').value.trim();
  if(!prompt) return;
  const streaming = document.getElementById('stream-mode').checked;

  document.getElementById('ask-btn').disabled = true;
  document.getElementById('loading').style.display = 'inline';
  document.getElementById('resp-box').textContent   = '';
  document.getElementById('resp-meta').style.display = 'none';

  log('→ "'+ prompt.substring(0,60)+(prompt.length>60?'...':'')+'"','i');

  if(streaming) {
    await sendStream(prompt);
  } else {
    await sendJSON(prompt);
  }

  document.getElementById('ask-btn').disabled = false;
  document.getElementById('loading').style.display = 'none';
  await refreshStatus();
}

async function sendJSON(prompt) {
  try {
    const data = await fetch('/chat',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({prompt})}).then(r=>r.json());
    document.getElementById('resp-box').textContent = data.text;
    showTier(data.tier, data.cached);
    tierCounts[data.tier] = (tierCounts[data.tier]||0)+1;
    updateDonut();
    const em={primary:'🧠',fallback:'⚡',cache:'💾',degraded:'🔕'};
    log('← '+em[data.tier]+' '+data.tier+(data.cached?' (cached)':''), data.tier==='degraded'?'e':data.tier!=='primary'?'w':'');
  } catch(e) {
    document.getElementById('resp-box').textContent='Request failed: '+e.message;
    log('✗ '+e.message,'e');
  }
}

async function sendStream(prompt) {
  const box = document.getElementById('resp-box');
  box.textContent = '';
  const url = '/chat/stream?prompt='+encodeURIComponent(prompt);
  const es  = new EventSource(url);
  let tier  = 'primary';
  es.onmessage = e => {
    const d = JSON.parse(e.data);
    if(d.done) {
      tier = d.tier || tier;
      es.close();
      showTier(tier, false);
      tierCounts[tier]=(tierCounts[tier]||0)+1;
      updateDonut();
      log('← 📡 stream via '+tier,'');
    } else {
      box.textContent += d.token;
    }
  };
  es.onerror = () => { es.close(); log('✗ stream error','e'); };
  // Wait for SSE to close
  await new Promise(res => { const orig=es.onerror; es.addEventListener('close',res); es.onerror=()=>{orig&&orig();res();}; setTimeout(res,90000); });
}

function showTier(tier, cached) {
  document.getElementById('resp-meta').style.display='flex';
  const tb=document.getElementById('tier-badge');
  tb.textContent=tier; tb.className='badge '+(tierColor[tier]||'gray');
  document.getElementById('cached-badge').style.display=cached?'inline':'none';
}

async function kill(which) {
  const url = which==='fallback' ? '/demo/kill-fallback' : '/demo/kill';
  await fetch(url,{method:'POST'});
  log('💀 '+which+' model killed','w');
  await refreshStatus();
}

async function restore(which) {
  const url = which==='fallback' ? '/demo/restore-fallback' : '/demo/restore';
  await fetch(url,{method:'POST'});
  log('✅ '+which+' model restored','');
  await refreshStatus();
}

document.getElementById('prompt').addEventListener('keydown',e=>{
  if(e.key==='Enter'&&(e.metaKey||e.ctrlKey)) sendPrompt();
});

checkOllama();
refreshStatus();
setInterval(refreshStatus,3000);
setInterval(checkOllama,15000);
</script>
</body>
</html>`
