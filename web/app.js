// Mini LLM Gateway — vanilla JS frontend.
// Talks to: GET /admin/providers, GET /admin/requests, POST /v1/chat/completions.

const $ = (id) => document.getElementById(id);

let messages = []; // running conversation history (sent verbatim to /v1/chat/completions)

async function loadProviders() {
  const resp = await fetch('/admin/providers');
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

async function loadLog() {
  const resp = await fetch('/admin/requests?limit=20');
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
  const assistantEl = appendMessage('assistant', '');

  try {
    const resp = await fetch('/v1/chat/completions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        provider: $('provider').value,
        model: $('model').value,
        stream: streamMode,
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

    // SSE events are separated by blank lines; within an event we read `data:` lines.
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
  await loadLog();

  $('send').addEventListener('click', sendMessage);
  $('clear').addEventListener('click', () => {
    messages = [];
    $('chat').innerHTML = '';
  });
  $('refresh').addEventListener('click', loadLog);

  $('input').addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      sendMessage();
    }
  });
});
