// Mini LLM Gateway — vanilla JS frontend.
// Talks to: GET /admin/providers, GET/POST/DELETE /admin/documents,
//           GET /admin/requests, POST /v1/chat/completions.

const $ = (id) => document.getElementById(id);

let messages = []; // running conversation history (sent verbatim to /v1/chat/completions)
let ragAvailable = false;
let authToken = localStorage.getItem('mlg_auth_token') || '';

// apiFetch wraps fetch so we send the Bearer token and prompt the user on 401.
// Retries the request once with the new token if the user provides one.
async function apiFetch(url, options = {}) {
  const headers = Object.assign({}, options.headers || {});
  if (authToken) headers['Authorization'] = 'Bearer ' + authToken;
  let resp = await fetch(url, { ...options, headers });
  if (resp.status === 401) {
    const t = window.prompt('This gateway requires a Bearer token (GATEWAY_AUTH_TOKEN). Enter it:');
    if (t) {
      authToken = t.trim();
      localStorage.setItem('mlg_auth_token', authToken);
      headers['Authorization'] = 'Bearer ' + authToken;
      resp = await fetch(url, { ...options, headers });
    }
  }
  return resp;
}

async function loadProviders() {
  const resp = await apiFetch('/admin/providers');
  const data = await resp.json();
  const sel = $('provider');
  sel.innerHTML = '';
  for (const p of data.providers) {
    const opt = document.createElement('option');
    opt.value = p.name;
    opt.textContent = p.name;
    if (p.name === data.default_provider) opt.selected = true;
    sel.appendChild(opt);
  }
  $('model').value = data.default_model;
}

async function loadDocuments() {
  const resp = await apiFetch('/admin/documents');
  if (resp.status === 503) {
    ragAvailable = false;
    $('rag').disabled = true;
    $('rag').checked = false;
    $('rag-status').textContent = 'RAG disabled (set GATEWAY_EMBEDDER to enable)';
    $('upload-details').open = false;
    $('upload-details').style.display = 'none';
    return;
  }
  ragAvailable = true;
  $('rag').disabled = false;
  $('rag-status').textContent = '';
  $('upload-details').style.display = '';
  const data = await resp.json();
  const tbody = $('docs-body');
  tbody.innerHTML = '';
  for (const d of (data.documents || [])) {
    const tr = document.createElement('tr');
    tr.appendChild(td(d.id, 'mono'));
    tr.appendChild(td(d.title));
    tr.appendChild(td(d.chunk_count));
    tr.appendChild(td(new Date(d.created_at).toLocaleString()));
    const delTd = document.createElement('td');
    const delBtn = document.createElement('button');
    delBtn.textContent = 'delete';
    delBtn.className = 'secondary tiny';
    delBtn.onclick = () => deleteDocument(d.id);
    delTd.appendChild(delBtn);
    tr.appendChild(delTd);
    tbody.appendChild(tr);
  }
}

async function uploadDocument() {
  const title = $('doc-title').value.trim();
  const body = $('doc-body').value.trim();
  if (!title || !body) {
    alert('Title and body are required');
    return;
  }
  $('upload').disabled = true;
  try {
    const resp = await apiFetch('/admin/documents', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title, body }),
    });
    if (!resp.ok) {
      const err = await resp.json().catch(() => ({}));
      alert(`Upload failed: ${err?.error?.message || resp.status}`);
      return;
    }
    $('doc-title').value = '';
    $('doc-body').value = '';
    await loadDocuments();
  } finally {
    $('upload').disabled = false;
  }
}

async function deleteDocument(id) {
  if (!confirm(`Delete document ${id}?`)) return;
  const resp = await apiFetch('/admin/documents/' + encodeURIComponent(id), { method: 'DELETE' });
  if (!resp.ok && resp.status !== 404) {
    alert(`Delete failed: HTTP ${resp.status}`);
    return;
  }
  await loadDocuments();
}

function fmtUSD(v) {
  if (v < 0.01) return '$' + v.toFixed(5).replace(/0+$/, '').replace(/\.$/, '.0');
  return '$' + v.toFixed(2);
}

async function loadCost() {
  const resp = await apiFetch('/admin/stats');
  if (!resp.ok) return;
  const data = await resp.json();
  $('cost-today').textContent = fmtUSD(data.today_usd || 0);
  $('cost-total').textContent = fmtUSD(data.total_usd || 0);

  const chart = $('cost-chart');
  chart.innerHTML = '';
  const models = data.by_model || [];
  if (models.length === 0) {
    const empty = document.createElement('div');
    empty.className = 'cost-empty';
    empty.textContent = 'No usage yet. Send a chat request to populate.';
    chart.appendChild(empty);
    return;
  }
  const maxUSD = Math.max(...models.map(m => m.usd), 0.000001);
  for (const m of models) {
    const row = document.createElement('div');
    row.className = 'cost-row' + (m.pricing_known ? '' : ' unknown');
    const name = document.createElement('div');
    name.className = 'name';
    name.textContent = m.model;
    const bar = document.createElement('div');
    bar.className = 'bar';
    const fill = document.createElement('div');
    fill.className = 'bar-fill';
    fill.style.width = (Math.max(0.5, (m.usd / maxUSD) * 100)) + '%';
    if (m.usd === 0) fill.style.background = '#bdc3c7';
    bar.appendChild(fill);
    const amt = document.createElement('div');
    amt.className = 'amount';
    amt.textContent = fmtUSD(m.usd);
    amt.title = `${m.requests} reqs · ${m.prompt_tokens} in / ${m.completion_tokens} out`;
    row.appendChild(name);
    row.appendChild(bar);
    row.appendChild(amt);
    chart.appendChild(row);
  }
}

async function loadLog() {
  const resp = await apiFetch('/admin/requests?limit=20');
  const data = await resp.json();
  const tbody = $('log-body');
  tbody.innerHTML = '';
  for (const r of data.requests) {
    const tr = document.createElement('tr');
    const time = new Date(r.ts).toLocaleTimeString();
    const statusClass = r.status_code >= 200 && r.status_code < 300 ? 'status-ok' : 'status-err';
    tr.appendChild(td(time));
    tr.appendChild(td(r.provider || '—'));
    tr.appendChild(td(r.model || '—'));
    tr.appendChild(td(r.status_code, statusClass));
    tr.appendChild(td(r.latency_ms));
    tr.appendChild(td(`${r.prompt_chars}/${r.completion_chars}`));
    tr.appendChild(td(`${r.prompt_tokens || 0}/${r.completion_tokens || 0}`));
    tr.appendChild(td(r.rag_chunk_ids ? r.rag_chunk_ids.split(',').length : ''));
    tr.appendChild(td(r.error || '', 'error'));
    tbody.appendChild(tr);
  }
}

function td(text, className) {
  const el = document.createElement('td');
  el.textContent = text;
  if (className) el.className = className;
  return el;
}

function appendMessage(role, content) {
  const div = document.createElement('div');
  div.className = `msg msg-${role}`;
  const roleEl = document.createElement('span');
  roleEl.className = 'role';
  roleEl.textContent = role;
  const contentEl = document.createElement('span');
  contentEl.className = 'content';
  contentEl.textContent = content;
  div.appendChild(roleEl);
  div.appendChild(contentEl);
  $('chat').appendChild(div);
  $('chat').scrollTop = $('chat').scrollHeight;
  return contentEl;
}

async function sendMessage() {
  const text = $('input').value.trim();
  if (!text) return;
  $('input').value = '';
  $('send').disabled = true;

  messages.push({ role: 'user', content: text });
  appendMessage('user', text);

  const streamMode = $('stream').checked;
  const useRAG = $('rag').checked && ragAvailable;
  const assistantEl = appendMessage('assistant', '');

  try {
    const resp = await apiFetch('/v1/chat/completions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        provider: $('provider').value,
        model: $('model').value,
        stream: streamMode,
        rag: useRAG,
        messages: messages,
      }),
    });

    if (!resp.ok) {
      const err = await resp.json().catch(() => ({}));
      const msg = err?.error?.message || `HTTP ${resp.status}`;
      assistantEl.textContent = `[error: ${msg}]`;
      assistantEl.parentElement.className = 'msg msg-error';
      return;
    }

    if (streamMode) {
      const acc = await consumeSSE(resp, (delta) => {
        assistantEl.textContent += delta;
        $('chat').scrollTop = $('chat').scrollHeight;
      });
      messages.push({ role: 'assistant', content: acc });
    } else {
      const data = await resp.json();
      const content = data?.choices?.[0]?.message?.content || '';
      assistantEl.textContent = content;
      messages.push({ role: 'assistant', content });
    }
  } catch (err) {
    assistantEl.textContent = `[network error: ${err.message}]`;
    assistantEl.parentElement.className = 'msg msg-error';
  } finally {
    $('send').disabled = false;
    loadLog();
    loadCost();
  }
}

// Read an SSE response body, invoke onDelta(text) for each content fragment,
// and return the accumulated string when [DONE] arrives or the stream ends.
async function consumeSSE(resp, onDelta) {
  const reader = resp.body.getReader();
  const decoder = new TextDecoder();
  let buf = '';
  let acc = '';

  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });

    let nl;
    while ((nl = buf.indexOf('\n')) >= 0) {
      const line = buf.slice(0, nl).trimEnd();
      buf = buf.slice(nl + 1);
      if (!line.startsWith('data:')) continue;
      const payload = line.slice(5).trim();
      if (payload === '[DONE]') return acc;
      let chunk;
      try { chunk = JSON.parse(payload); } catch { continue; }
      if (chunk.error) {
        acc += `\n[error: ${chunk.error.message || 'upstream error'}]`;
        onDelta(`\n[error: ${chunk.error.message || 'upstream error'}]`);
        continue;
      }
      const delta = chunk.choices?.[0]?.delta?.content;
      if (delta) {
        acc += delta;
        onDelta(delta);
      }
    }
  }
  return acc;
}

window.addEventListener('DOMContentLoaded', async () => {
  await loadProviders();
  await loadDocuments();
  await loadLog();
  await loadCost();

  $('send').addEventListener('click', sendMessage);
  $('clear').addEventListener('click', () => {
    messages = [];
    $('chat').innerHTML = '';
  });
  $('refresh').addEventListener('click', loadLog);
  $('refresh-cost').addEventListener('click', loadCost);
  $('upload').addEventListener('click', uploadDocument);

  $('input').addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendMessage();
    }
  });
});
