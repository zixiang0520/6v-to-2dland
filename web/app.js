const api = (p, opt) => fetch(p, opt).then(r => r.json().then(d => ({ ok: r.ok, d })));
const selected = new Map(); // magnet -> {name, magnet, category, title}

function esc(s) {
  return String(s || '').replace(/[<>&"]/g, c => ({ '<': '&lt;', '>': '&gt;', '&': '&amp;', '"': '&quot;' }[c]));
}
function statusText(s) { return ['等待中', '下载中', '已完成', '失败'][s] || ('状态' + s); }

// —— 登录状态 ——
async function refreshAuth() {
  const { ok, d } = await api('/api/auth/status');
  const el = document.getElementById('authStatus');
  const btn = document.getElementById('btnLogin');
  if (!ok) { el.textContent = '状态未知'; return; }
  btn.disabled = !d.has_credentials;
  if (!d.has_credentials) { el.textContent = '未配置凭证（见 config.json）'; return; }
  el.textContent = d.logged_in ? '已登录' : '未登录';
}

document.getElementById('btnLogin').onclick = async () => {
  const { ok, d } = await api('/api/auth/login', { method: 'POST' });
  if (!ok) { alert(d.error || '登录失败'); return; }
  document.getElementById('mUri').textContent = d.verification_uri;
  document.getElementById('mUri').href = d.verification_uri;
  document.getElementById('mCode').textContent = d.user_code;
  document.getElementById('mState').textContent = '等待授权…';
  document.getElementById('modal').classList.remove('hidden');
  pollAuth(d.interval || 5);
};
async function pollAuth(interval) {
  const timer = setInterval(async () => {
    const { ok, d } = await api('/api/auth/poll');
    if (!ok) return;
    document.getElementById('mState').textContent = '状态：' + d.status;
    if (d.logged_in) {
      clearInterval(timer);
      document.getElementById('mState').textContent = '登录成功！';
      refreshAuth();
      setTimeout(() => document.getElementById('modal').classList.add('hidden'), 1200);
    }
  }, interval * 1000);
};
document.getElementById('mClose').onclick = () => document.getElementById('modal').classList.add('hidden');

// —— 搜索 ——
document.getElementById('btnSearch').onclick = doSearch;
document.getElementById('kw').addEventListener('keydown', e => { if (e.key === 'Enter') doSearch(); });

async function doSearch() {
  const q = document.getElementById('kw').value.trim();
  if (!q) return;
  const hint = document.getElementById('searchHint');
  const res = document.getElementById('results');
  hint.textContent = '搜索中（全分类并发爬取，约需数十秒）…';
  res.innerHTML = '';
  const { ok, d } = await api('/api/search?q=' + encodeURIComponent(q));
  hint.textContent = '';
  if (!ok) { res.innerHTML = '<p class="err">' + esc(d.error) + '</p>'; return; }
  if (!d.length) { res.innerHTML = '<p>未找到匹配资源</p>'; return; }
  res.innerHTML = d.map((r, i) =>
    `<div class="res"><div class="res-head">
      <span class="date">${esc(r.date)}</span><span class="cat">${esc(r.category)}</span>
      <a href="${esc(r.url)}" target="_blank">${esc(r.title)}</a>
      <button data-i="${i}" class="btn-mag ghost">查看磁力链</button>
    </div><div class="mags" id="mags-${i}"></div></div>`).join('');
  d.forEach((r, i) => {
    document.querySelector(`[data-i="${i}"]`).onclick = () => toggleMags(i, r);
  });
  window._last = d;
}

async function toggleMags(i, r) {
  const box = document.getElementById('mags-' + i);
  if (box.innerHTML) { box.innerHTML = ''; return; }
  box.innerHTML = '<p class="hint">加载磁力链…</p>';
  const { ok, d } = await api('/api/magnets?url=' + encodeURIComponent(r.url));
  if (!ok) { box.innerHTML = '<p class="err">' + esc(d.error) + '</p>'; return; }
  if (!d.length) { box.innerHTML = '<p>该页未提取到磁力链</p>'; return; }
  box.innerHTML = d.map(m =>
    `<label class="mag"><input type="checkbox" data-m="${esc(m.magnet)}">
     <span class="mag-name">${esc(m.desc)}</span><code>${esc(m.magnet.slice(0, 70))}…</code></label>`).join('');
  const checks = box.querySelectorAll('input[type=checkbox]');
  d.forEach((m, idx) => {
    const cb = checks[idx];
    cb.checked = selected.has(m.magnet);
    cb.onchange = () => {
      if (cb.checked) {
        selected.set(m.magnet, { name: m.name, magnet: m.magnet, category: r.category, title: r.title });
      } else {
        selected.delete(m.magnet);
      }
      renderSelected();
    };
  });
}

function renderSelected() {
  const box = document.getElementById('selected');
  document.getElementById('selCount').textContent = selected.size;
  document.getElementById('btnPush').disabled = selected.size === 0;
  if (!selected.size) { box.className = 'hint'; box.textContent = '暂未选择'; return; }
  box.className = '';
  box.innerHTML = [...selected.values()].map(m =>
    `<div>• [${esc(m.category)}] ${esc(m.name)}</div>`).join('');
}

// —— 推送 ——
document.getElementById('btnPush').onclick = async () => {
  const magnets = [...selected.values()];
  const pr = document.getElementById('pushResult');
  pr.innerHTML = '<p class="hint">推送中（含 TMDB 规范化与建目录）…</p>';
  const { ok, d } = await api('/api/push', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ magnets })
  });
  if (!ok) { pr.innerHTML = '<p class="err">' + esc(d.error) + '</p>'; return; }
  const okN = d.items.filter(x => x.ok).length;
  pr.innerHTML = `<p>已推送 ${okN}/${d.items.length} 条</p>` +
    d.items.map(x => `<div class="${x.ok ? 'ok' : 'err'}">${esc(x.folder)}${x.season ? ' / ' + esc(x.season) : ''}：${x.ok ? '成功 → ' + esc(x.save_path) : esc(x.error)}</div>`).join('');
  loadTasks();
};

// —— 任务 ——
document.getElementById('btnTasks').onclick = loadTasks;
async function loadTasks() {
  const box = document.getElementById('tasks');
  box.className = '';
  box.innerHTML = '<p class="hint">加载中…</p>';
  const { ok, d } = await api('/api/tasks');
  if (!ok) { box.innerHTML = '<p class="err">' + esc(d.error) + '</p>'; return; }
  if (!d || !d.length) { box.innerHTML = '<p class="hint">暂无任务</p>'; return; }
  box.innerHTML = d.map(t =>
    `<div class="task"><span class="t-name">${esc(t.name || t.url)}</span>
     <span class="prog">${t.progress || 0}%</span>
     <span class="status">${statusText(t.status)}</span></div>`).join('');
}

refreshAuth();
