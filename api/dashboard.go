package api

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>AgentShield — Resilient LLM Demo</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: 'Segoe UI', system-ui, sans-serif; background: #0f1117; color: #e2e8f0; min-height: 100vh; }
  .header { background: #1a1d2e; border-bottom: 1px solid #2d3748; padding: 16px 32px; display: flex; align-items: center; gap: 12px; }
  .header h1 { font-size: 20px; font-weight: 700; color: #fff; }
  .header .subtitle { font-size: 13px; color: #718096; }
  .badge { padding: 3px 10px; border-radius: 20px; font-size: 11px; font-weight: 600; letter-spacing: 0.5px; }
  .badge-green { background: #22543d; color: #68d391; }
  .badge-red { background: #742a2a; color: #fc8181; }
  .badge-yellow { background: #744210; color: #f6e05e; }
  .badge-gray { background: #2d3748; color: #a0aec0; }
  .container { max-width: 1100px; margin: 0 auto; padding: 32px; display: grid; grid-template-columns: 1fr 380px; gap: 24px; }
  .card { background: #1a1d2e; border: 1px solid #2d3748; border-radius: 12px; padding: 20px; }
  .card h2 { font-size: 14px; font-weight: 600; color: #a0aec0; text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 16px; }
  .chain { display: flex; flex-direction: column; gap: 8px; }
  .tier { display: flex; align-items: center; gap: 12px; padding: 12px 16px; border-radius: 8px; border: 1px solid #2d3748; background: #0f1117; transition: all 0.3s; }
  .tier.active { border-color: #4299e1; background: #1a365d22; }
  .tier.active .tier-icon { color: #4299e1; }
  .tier-icon { font-size: 18px; width: 28px; text-align: center; }
  .tier-info { flex: 1; }
  .tier-name { font-size: 14px; font-weight: 600; color: #e2e8f0; }
  .tier-desc { font-size: 12px; color: #718096; margin-top: 2px; }
  .tier-arrow { color: #4a5568; font-size: 12px; margin: 0 4px 0 0; }
  .prompt-area textarea { width: 100%; background: #0f1117; border: 1px solid #2d3748; border-radius: 8px; color: #e2e8f0; padding: 12px; font-size: 14px; resize: vertical; min-height: 80px; outline: none; }
  .prompt-area textarea:focus { border-color: #4299e1; }
  .btn { padding: 10px 20px; border-radius: 8px; border: none; cursor: pointer; font-size: 14px; font-weight: 600; transition: all 0.2s; }
  .btn-blue { background: #3182ce; color: #fff; }
  .btn-blue:hover { background: #2b6cb0; }
  .btn-blue:disabled { background: #2d3748; color: #718096; cursor: not-allowed; }
  .btn-red { background: #c53030; color: #fff; }
  .btn-red:hover { background: #9b2c2c; }
  .btn-green { background: #276749; color: #fff; }
  .btn-green:hover { background: #22543d; }
  .response-box { background: #0f1117; border: 1px solid #2d3748; border-radius: 8px; padding: 16px; min-height: 100px; font-size: 14px; line-height: 1.6; white-space: pre-wrap; }
  .response-meta { display: flex; gap: 8px; align-items: center; margin-bottom: 8px; }
  .stat-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
  .stat { background: #0f1117; border: 1px solid #2d3748; border-radius: 8px; padding: 12px; text-align: center; }
  .stat-value { font-size: 22px; font-weight: 700; color: #e2e8f0; }
  .stat-label { font-size: 11px; color: #718096; margin-top: 4px; }
  .demo-btns { display: flex; gap: 8px; flex-wrap: wrap; }
  .log { font-family: monospace; font-size: 12px; background: #0f1117; border: 1px solid #2d3748; border-radius: 8px; padding: 12px; max-height: 180px; overflow-y: auto; color: #68d391; }
  .log-entry { margin-bottom: 4px; }
  .log-entry.warn { color: #f6e05e; }
  .log-entry.error { color: #fc8181; }
  .log-entry.info { color: #63b3ed; }
  .divider { border: none; border-top: 1px solid #2d3748; margin: 16px 0; }
  .full-width { grid-column: 1 / -1; }
  .gap { margin-top: 12px; }
</style>
</head>
<body>
<div class="header">
  <div>
    <h1>⚡ AgentShield</h1>
    <div class="subtitle">Resilient LLM Agent — powered by flowguard</div>
  </div>
  <div style="margin-left: auto; display: flex; gap: 8px; align-items: center;">
    <span id="ollama-status" class="badge badge-gray">checking...</span>
  </div>
</div>

<div class="container">
  <!-- Left column -->
  <div style="display:flex;flex-direction:column;gap:24px;">

    <div class="card">
      <h2>Ask the Agent</h2>
      <div class="prompt-area">
        <textarea id="prompt" placeholder="Ask anything... e.g. 'Explain circuit breakers in one paragraph'"></textarea>
      </div>
      <div class="gap" style="display:flex;gap:8px;">
        <button class="btn btn-blue" id="ask-btn" onclick="sendPrompt()">Send →</button>
        <span id="loading" style="display:none;color:#718096;font-size:13px;align-self:center;">thinking...</span>
      </div>
      <div class="gap">
        <div class="response-meta" id="response-meta" style="display:none;">
          <span style="font-size:12px;color:#718096;">answered by</span>
          <span id="tier-badge" class="badge"></span>
          <span id="cached-badge" class="badge badge-yellow" style="display:none;">cached</span>
        </div>
        <div class="response-box" id="response-box" style="color:#718096;">Response will appear here...</div>
      </div>
    </div>

    <div class="card">
      <h2>Demo Controls</h2>
      <p style="font-size:13px;color:#718096;margin-bottom:12px;">Simulate failures to see the degradation chain in action.</p>
      <div class="demo-btns">
        <button class="btn btn-red" onclick="killPrimary()">💀 Kill Primary Model</button>
        <button class="btn btn-green" onclick="restorePrimary()">✅ Restore Primary</button>
      </div>
      <hr class="divider">
      <h2>Event Log</h2>
      <div class="log" id="log"></div>
    </div>

  </div>

  <!-- Right column -->
  <div style="display:flex;flex-direction:column;gap:24px;">

    <div class="card">
      <h2>Degradation Chain</h2>
      <div class="chain">
        <div class="tier" id="tier-primary">
          <span class="tier-icon">🧠</span>
          <div class="tier-info">
            <div class="tier-name">Primary — llama3.2</div>
            <div class="tier-desc">Circuit breaker + 3x retry</div>
          </div>
          <span id="cb-primary" class="badge badge-green">closed</span>
        </div>
        <div style="padding-left:14px;color:#4a5568;font-size:12px;">↓ on failure</div>
        <div class="tier" id="tier-fallback">
          <span class="tier-icon">⚡</span>
          <div class="tier-info">
            <div class="tier-name">Fallback — llama3.2:1b</div>
            <div class="tier-desc">Smaller, faster model</div>
          </div>
          <span id="cb-fallback" class="badge badge-green">closed</span>
        </div>
        <div style="padding-left:14px;color:#4a5568;font-size:12px;">↓ on failure</div>
        <div class="tier" id="tier-cache">
          <span class="tier-icon">💾</span>
          <div class="tier-info">
            <div class="tier-name">Response Cache</div>
            <div class="tier-desc">10-minute in-memory TTL</div>
          </div>
          <span id="cache-size" class="badge badge-gray">0 entries</span>
        </div>
        <div style="padding-left:14px;color:#4a5568;font-size:12px;">↓ on cache miss</div>
        <div class="tier" id="tier-degraded">
          <span class="tier-icon">🔕</span>
          <div class="tier-info">
            <div class="tier-name">Graceful Denial</div>
            <div class="tier-desc">Always available</div>
          </div>
          <span class="badge badge-gray">last resort</span>
        </div>
      </div>
    </div>

    <div class="card">
      <h2>Live Stats</h2>
      <div class="stat-grid">
        <div class="stat">
          <div class="stat-value" id="total-requests">0</div>
          <div class="stat-label">Total Requests</div>
        </div>
        <div class="stat">
          <div class="stat-value" id="error-rate">0%</div>
          <div class="stat-label">Primary Error Rate</div>
        </div>
        <div class="stat">
          <div class="stat-value" id="cache-entries">0</div>
          <div class="stat-label">Cache Entries</div>
        </div>
        <div class="stat">
          <div class="stat-value" id="active-tier">—</div>
          <div class="stat-label">Last Active Tier</div>
        </div>
      </div>
    </div>

  </div>
</div>

<script>
const tierColors = { primary: 'badge-green', fallback: 'badge-yellow', cache: 'badge-yellow', degraded: 'badge-red' };
const cbColors = { closed: 'badge-green', open: 'badge-red', 'half-open': 'badge-yellow', killed: 'badge-red' };

function log(msg, type = '') {
  const el = document.getElementById('log');
  const ts = new Date().toLocaleTimeString();
  el.innerHTML = '<div class="log-entry ' + type + '">[' + ts + '] ' + msg + '</div>' + el.innerHTML;
}

async function sendPrompt() {
  const prompt = document.getElementById('prompt').value.trim();
  if (!prompt) return;

  document.getElementById('ask-btn').disabled = true;
  document.getElementById('loading').style.display = 'inline';
  document.getElementById('response-box').textContent = '...';
  document.getElementById('response-meta').style.display = 'none';

  log('→ Sending: "' + prompt.substring(0, 60) + (prompt.length > 60 ? '...' : '') + '"', 'info');

  try {
    const res = await fetch('/chat', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ prompt })
    });
    const data = await res.json();

    document.getElementById('response-box').textContent = data.text;
    document.getElementById('response-meta').style.display = 'flex';

    const badge = document.getElementById('tier-badge');
    badge.textContent = data.tier;
    badge.className = 'badge ' + (tierColors[data.tier] || 'badge-gray');

    const cachedBadge = document.getElementById('cached-badge');
    cachedBadge.style.display = data.cached ? 'inline' : 'none';

    document.getElementById('active-tier').textContent = data.tier;

    const emoji = { primary: '🧠', fallback: '⚡', cache: '💾', degraded: '🔕' };
    log('← Response via ' + emoji[data.tier] + ' ' + data.tier + (data.cached ? ' (cached)' : ''),
      data.tier === 'degraded' ? 'error' : data.tier !== 'primary' ? 'warn' : '');

    await refreshStatus();
  } catch (e) {
    document.getElementById('response-box').textContent = 'Request failed: ' + e.message;
    log('✗ Request failed: ' + e.message, 'error');
  } finally {
    document.getElementById('ask-btn').disabled = false;
    document.getElementById('loading').style.display = 'none';
  }
}

async function killPrimary() {
  await fetch('/demo/kill', { method: 'POST' });
  log('💀 Primary model killed (simulated)', 'warn');
  await refreshStatus();
}

async function restorePrimary() {
  await fetch('/demo/restore', { method: 'POST' });
  log('✅ Primary model restored', '');
  await refreshStatus();
}

async function refreshStatus() {
  try {
    const res = await fetch('/status');
    const s = await res.json();

    const cbP = document.getElementById('cb-primary');
    const primaryLabel = s.primary_killed ? 'killed' : s.primary_breaker;
    cbP.textContent = primaryLabel;
    cbP.className = 'badge ' + (cbColors[primaryLabel] || 'badge-gray');

    const cbF = document.getElementById('cb-fallback');
    cbF.textContent = s.fallback_breaker;
    cbF.className = 'badge ' + (cbColors[s.fallback_breaker] || 'badge-gray');

    document.getElementById('cache-size').textContent = s.cache_size + ' entries';
    document.getElementById('total-requests').textContent = s.total_requests;
    document.getElementById('error-rate').textContent = (s.error_rate * 100).toFixed(0) + '%';
    document.getElementById('cache-entries').textContent = s.cache_size;
  } catch (_) {}
}

async function checkOllama() {
  try {
    const res = await fetch('/health');
    const badge = document.getElementById('ollama-status');
    if (res.ok) {
      badge.textContent = 'Ollama online';
      badge.className = 'badge badge-green';
    } else {
      badge.textContent = 'Ollama offline';
      badge.className = 'badge badge-red';
    }
  } catch (_) {
    document.getElementById('ollama-status').textContent = 'Ollama offline';
    document.getElementById('ollama-status').className = 'badge badge-red';
  }
}

document.getElementById('prompt').addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) sendPrompt();
});

checkOllama();
refreshStatus();
setInterval(refreshStatus, 3000);
setInterval(checkOllama, 10000);
</script>
</body>
</html>`
