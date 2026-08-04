'use strict';
(function () {
  // ============ 工具 ============
  const $ = (s, r = document) => r.querySelector(s);
  const $$ = (s, r = document) => Array.from(r.querySelectorAll(s));
  const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  async function copy(text) {
    try { await navigator.clipboard.writeText(text); toast('已复制到剪贴板', 'success'); }
    catch { const t = document.createElement('textarea'); t.value = text; document.body.appendChild(t); t.select(); document.execCommand('copy'); t.remove(); toast('已复制', 'success'); }
  }

  // 6v 分类中文映射（与后端 categoryNames 保持一致）
  const catNames = { dy: '电影', gydy: '国语电影', gq: '经典高清', zydy: '动漫', jddy: '动画电影', '3D': '3D电影', dlz: '国剧', rj: '日韩剧', mj: '欧美剧', zy: '综艺', shoujidianyingmp4: '手机电影' };
  const catName = c => catNames[c] || c || '未分类';
  // 2dland 任务状态：0=等待 1=解析 2=下载中 1000=已完成（实测）
  const taskStatus = s => {
    if (s === 1000) return '已完成';
    if (s === 0) return '等待中';
    if (s === 1) return '解析中';
    if (s === 2) return '下载中';
    if (s === 3) return '失败';
    return '状态' + s;
  };

  // ============ API ============
  const api = {
    async call(path, opts = {}) {
      let res;
      try {
        res = await fetch(path, {
          headers: { 'Content-Type': 'application/json', ...(opts.headers || {}) },
          ...opts,
        });
      } catch (e) {
        return { ok: false, status: 0, data: { error: '网络错误：' + e.message } };
      }
      const txt = await res.text();
      let data;
      try { data = txt ? JSON.parse(txt) : {}; } catch { data = { error: txt || '响应解析失败' }; }
      if (res.status === 401 && state.uiSession) {
        state.uiSession.logged_in = false;
        toast('会话已过期，请重新登录', 'warn');
        render();
      }
      return { ok: res.ok, status: res.status, data };
    },
    get(p) { return this.call(p); },
    post(p, body) { return this.call(p, { method: 'POST', body: JSON.stringify(body || {}) }); },
  };

  // ============ 状态 ============
  const state = {
    uiSession: null,
    auth: { logged_in: false, has_credentials: false },
    settings: null,
    view: 'search',
    selected: new Map(),   // magnet -> {name, magnet, category, title}
    lastResults: [],
    taskSel: new Set(),    // 选中的任务 identity（批量删除用）
    taskAll: [],           // 全部任务（后端已分页拉全）
    taskPage: 1,           // 当前任务页码（前端 50/页）
    fileCwd: '/',          // 文件管理当前目录路径
    fileSel: new Set(),    // 选中的文件 identity
    fileItems: [],          // 当前目录文件列表
    homeCats: [],           // 6v520 各分类条目 [{category,name,items}]（每分类前 100 条）
    homeActiveCat: null,    // 当前选中的分类目录名
    pendingHome: null,      // 从发现页点击跳转到搜索页时携带的 {title,url,category}
    theme: 'auto',
    pollTimer: null,
    taskRefreshTimer: null,  // 任务页自动刷新定时器
  };

  // ============ 主题 ============
  function initTheme() {
    state.theme = localStorage.getItem('theme') || 'auto';
    applyTheme();
    matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
      if (state.theme === 'auto') applyTheme();
    });
  }
  function applyTheme() {
    const isDark = state.theme === 'dark' || (state.theme === 'auto' && matchMedia('(prefers-color-scheme: dark)').matches);
    document.documentElement.setAttribute('data-theme', isDark ? 'dark' : 'light');
  }
  function toggleTheme() {
    const cur = document.documentElement.getAttribute('data-theme');
    state.theme = cur === 'dark' ? 'light' : 'dark';
    localStorage.setItem('theme', state.theme);
    applyTheme();
    const btn = $('#themeBtn');
    if (btn) btn.textContent = state.theme === 'dark' || (state.theme === 'auto' && matchMedia('(prefers-color-scheme: dark)').matches) ? '☀️' : '🌙';
  }

  // ============ Toast ============
  function toast(msg, type = 'info', ms = 3000) {
    const el = document.createElement('div');
    el.className = `toast ${type}`;
    el.textContent = msg;
    $('#toast').appendChild(el);
    setTimeout(() => { el.style.transition = 'opacity .25s, transform .25s'; el.style.opacity = '0'; el.style.transform = 'translateX(20px)'; setTimeout(() => el.remove(), 260); }, ms);
  }

  // ============ 初始化 ============
  async function init() {
    initTheme();
    window.addEventListener('hashchange', onHash);
    const { ok, data } = await api.get('/api/ui/session');
    if (ok) state.uiSession = data;
    if (state.uiSession && state.uiSession.logged_in) {
      await refreshAuth();
      onHash();
    } else {
      render();
    }
  }

  function onHash() {
    const h = location.hash.slice(1);
    state.view = ['home', 'search', 'tasks', 'files', 'settings'].includes(h) ? h : 'home';
    render();
  }

  // ============ 渲染入口 ============
  function render() {
    const app = $('#app');
    if (!state.uiSession) {
      app.innerHTML = '<div class="boot"><div class="spinner lg"></div><p class="muted">正在加载…</p></div>';
      return;
    }
    if (!state.uiSession.auth_required) { app.innerHTML = viewSetup(); bindSetup(); return; }
    if (!state.uiSession.logged_in) { app.innerHTML = viewLogin(); bindLogin(); return; }
    app.innerHTML = viewShell();
    bindShell();
    renderContent();
  }

  function renderContent() {
    const c = $('#content');
    if (!c) return;
    if (state.view !== 'tasks') stopTaskAutoRefresh();
    if (state.view === 'home') { c.innerHTML = viewHome(); bindHome(); loadHome(); }
    else if (state.view === 'tasks') { c.innerHTML = viewTasks(); bindTasks(); loadTasks(); }
    else if (state.view === 'files') { c.innerHTML = viewFiles(); bindFiles(); loadFiles(); }
    else if (state.view === 'settings') { c.innerHTML = viewSettingsLoading(); loadSettings(); }
    else { c.innerHTML = viewSearch(); bindSearch(); renderSelected(); }
  }

  // ============ 视图：UI 登录 ============
  function viewLogin() {
    return `
    <div class="auth-screen">
      <div class="auth-card">
        <div class="logo">🔐</div>
        <h1>访问登录</h1>
        <p class="subtitle">请输入访问密码以进入控制台</p>
        <form id="loginForm">
          <div class="field">
            <input type="password" id="pwd" class="input" placeholder="访问密码" autocomplete="current-password" autofocus>
          </div>
          <button type="submit" class="btn primary block lg">登 录</button>
        </form>
      </div>
    </div>`;
  }
  function bindLogin() {
    $('#loginForm').onsubmit = async e => {
      e.preventDefault();
      const btn = $('#loginForm button[type=submit]');
      btn.disabled = true; btn.textContent = '登录中…';
      const { ok, data } = await api.post('/api/ui/login', { password: $('#pwd').value });
      if (!ok) { toast(data.error || '登录失败', 'error'); btn.disabled = false; btn.textContent = '登 录'; return; }
      state.uiSession = { auth_required: true, logged_in: true };
      await refreshAuth();
      render();
    };
  }

  // ============ 视图：首次设置向导 ============
  function viewSetup() {
    return `
    <div class="auth-screen">
      <div class="auth-card wide">
        <div class="logo">🚀</div>
        <h1>初始化配置</h1>
        <p class="subtitle">首次使用，请完成以下设置（之后可在「设置」页修改）</p>
        <form id="setupForm">
          <div class="section-title">访问安全</div>
          <div class="field">
            <label>访问密码</label>
            <input type="password" id="password" class="input" placeholder="设置访问本控制台的密码" autocomplete="new-password">
            <div class="help">用于保护本 Web UI，建议使用强密码。</div>
          </div>

          <div class="section-title">2dland 凭证</div>
          <div class="field-row">
            <div class="field"><label>Client ID</label><input id="client_id" class="input" placeholder="2dland 开放平台 client_id"></div>
            <div class="field"><label>Client Secret</label><input type="password" id="client_secret" class="input" placeholder="client_secret"></div>
          </div>
          <div class="help" style="margin-top:-8px;margin-bottom:16px;">在 2dland 开放平台创建应用后获取，用于调用离线下载 API。</div>

          <div class="section-title">TMDB（可选，用于规范化标题与年份）</div>
          <div class="field"><label>TMDB API Key</label><input id="tmdb_api_key" class="input" placeholder="留空则不启用标题规范化"></div>
          <div class="field"><label>TMDB 代理服务器</label><input id="tmdb_proxy" class="input" placeholder="如 http://127.0.0.1:7890（TMDB 在国内通常需代理）"></div>

          <button type="submit" class="btn primary block lg">完成初始化</button>
        </form>
      </div>
    </div>`;
  }
  function bindSetup() {
    $('#setupForm').onsubmit = async e => {
      e.preventDefault();
      const body = {
        password: $('#password').value,
        client_id: $('#client_id').value.trim(),
        client_secret: $('#client_secret').value.trim(),
        tmdb_api_key: $('#tmdb_api_key').value.trim(),
        tmdb_proxy: $('#tmdb_proxy').value.trim(),
      };
      if (!body.password) { toast('请设置访问密码', 'error'); return; }
      if (!body.client_id || !body.client_secret) { toast('请填写 2dland client_id / client_secret', 'error'); return; }
      const btn = $('#setupForm button[type=submit]');
      btn.disabled = true; btn.textContent = '保存中…';
      const { ok, data } = await api.post('/api/ui/setup', body);
      if (!ok) { toast(data.error || '初始化失败', 'error'); btn.disabled = false; btn.textContent = '完成初始化'; return; }
      toast('初始化完成，欢迎使用！', 'success');
      state.uiSession = { auth_required: true, logged_in: true };
      await refreshAuth();
      location.hash = 'home';
      render();
    };
  }

  // ============ 视图：主框架 ============
  function viewShell() {
    const a = state.auth;
    const authPill = a.logged_in
      ? '<span class="pill ok"><span class="dot"></span>2dland 已登录</span>'
      : (a.has_credentials
        ? '<span class="pill warn"><span class="dot"></span>2dland 未登录</span>'
        : '<span class="pill"><span class="dot"></span>未配置凭证</span>');
    const isDark = document.documentElement.getAttribute('data-theme') === 'dark';
    return `
    <div class="app-shell">
      <header class="topbar">
        <div class="brand"><span class="mark">6v</span><span>6v → 2dland</span></div>
        <nav class="nav">
          <a class="nav-item ${state.view === 'home' ? 'active' : ''}" href="#home">🏠 发现</a>
          <a class="nav-item ${state.view === 'search' ? 'active' : ''}" href="#search">🔍 搜索</a>
          <a class="nav-item ${state.view === 'tasks' ? 'active' : ''}" href="#tasks">📋 任务</a>
          <a class="nav-item ${state.view === 'files' ? 'active' : ''}" href="#files">📂 文件</a>
          <a class="nav-item ${state.view === 'settings' ? 'active' : ''}" href="#settings">⚙ 设置</a>
        </nav>
        <div class="actions">
          ${authPill}
          <button class="icon-btn" id="themeBtn" title="切换主题">${isDark ? '☀️' : '🌙'}</button>
          <button class="btn sm ghost" id="logoutBtn">退出</button>
        </div>
      </header>
      <main class="content" id="content"></main>
      <nav class="mobile-nav">
        <a class="m-nav-item ${state.view === 'home' ? 'active' : ''}" href="#home"><span class="m-ico">🏠</span><span class="m-lbl">发现</span></a>
        <a class="m-nav-item ${state.view === 'search' ? 'active' : ''}" href="#search"><span class="m-ico">🔍</span><span class="m-lbl">搜索</span></a>
        <a class="m-nav-item ${state.view === 'tasks' ? 'active' : ''}" href="#tasks"><span class="m-ico">📋</span><span class="m-lbl">任务</span></a>
        <a class="m-nav-item ${state.view === 'files' ? 'active' : ''}" href="#files"><span class="m-ico">📂</span><span class="m-lbl">文件</span></a>
        <a class="m-nav-item ${state.view === 'settings' ? 'active' : ''}" href="#settings"><span class="m-ico">⚙</span><span class="m-lbl">设置</span></a>
      </nav>
    </div>`;
  }
  function bindShell() {
    $('#themeBtn').onclick = toggleTheme;
    $('#logoutBtn').onclick = async () => {
      await api.post('/api/ui/logout');
      state.uiSession = { auth_required: true, logged_in: false };
      state.selected.clear();
      stopTaskAutoRefresh();
      render();
    };
  }

  // ============ 视图：发现（6v520 各分类列表，纯文字） ============
  // 抓取策略：首屏 GET /api/home 拿所有分类各 20 条概览；
  // 用户点击某个分类标签时，再 GET /api/home?cat=xxx 拉取该分类前 100 条完整列表。
  function viewHome() {
    return `
    <div class="page-head">
      <div><h2>发现</h2><div class="desc">浏览 6v520 各分类最新资源，点击标题直达磁力链</div></div>
      <button class="btn" id="btnRefreshHome">⟳ 刷新</button>
    </div>
    <div id="homeTabs" class="home-tabs"></div>
    <div id="homeGrid" class="home-list"><div class="loading-box"><span class="spinner"></span> 正在抓取各分类资源（11 分类并发，约 5~10 秒）…</div></div>`;
  }
  function bindHome() {
    const btn = $('#btnRefreshHome');
    if (btn) btn.onclick = () => {
      state.homeActiveCat = null;
      loadHome();
    };
  }
  // loadHome：并发抓取所有分类，每分类前 100 条（一次拉满，无二次请求）。
  async function loadHome() {
    const grid = $('#homeGrid'), tabs = $('#homeTabs');
    if (grid) grid.innerHTML = '<div class="loading-box"><span class="spinner"></span> 正在抓取各分类前 100 条（11 分类并发，约 10~20 秒）…</div>';
    if (tabs) tabs.innerHTML = '';
    const { ok, data } = await api.get('/api/home');
    if (!ok) { if (grid) grid.innerHTML = `<div class="empty"><div class="ico">⚠️</div>${esc(data.error || '加载失败')}</div>`; return; }
    state.homeCats = data || [];
    if (!state.homeCats.length) { if (grid) grid.innerHTML = '<div class="empty"><div class="ico">📭</div>未抓取到内容</div>'; return; }
    if (state.homeActiveCat == null || !state.homeCats.some(c => c.category === state.homeActiveCat)) {
      state.homeActiveCat = state.homeCats[0].category;
    }
    renderHomeTabs();
    renderHomeGrid();
  }
  function renderHomeTabs() {
    const tabs = $('#homeTabs');
    if (!tabs) return;
    tabs.innerHTML = state.homeCats.map(c =>
      `<button class="home-tab ${c.category === state.homeActiveCat ? 'active' : ''}" data-cat="${esc(c.category)}">${esc(c.name)}<span class="home-tab-count">${c.items.length}</span></button>`
    ).join('');
    $$('.home-tab').forEach(b => b.onclick = () => {
      if (state.homeActiveCat === b.dataset.cat) return;
      state.homeActiveCat = b.dataset.cat;
      renderHomeTabs();
      renderHomeGrid();
    });
  }
  // renderHomeGrid：纯文字列表，每行显示日期 + 标题。
  function renderHomeGrid() {
    const grid = $('#homeGrid');
    if (!grid) return;
    const cat = state.homeCats.find(c => c.category === state.homeActiveCat);
    if (!cat) { grid.innerHTML = ''; return; }
    const items = cat.items || [];
    if (!items.length) { grid.innerHTML = '<div class="empty"><div class="ico">📭</div>该分类暂无内容</div>'; return; }
    grid.innerHTML = items.map((it, i) => {
      const d = it.date ? it.date.slice(5) : ''; // MM-DD
      return `<div class="home-row" data-idx="${i}" title="${esc(it.title)}">
        ${d ? `<span class="home-date">${esc(d)}</span>` : ''}
        <span class="home-text">${esc(it.title)}</span>
      </div>`;
    }).join('');
    $$('.home-row').forEach(el => el.onclick = () => clickHome(+el.dataset.idx));
  }
  // clickHome：点击发现页条目 → 跳转搜索页 → 自动用标题搜索并展开磁力链。
  function clickHome(i) {
    const cat = state.homeCats.find(c => c.category === state.homeActiveCat);
    if (!cat) return;
    const it = cat.items[i];
    if (!it) return;
    state.pendingHome = { title: it.title, url: it.url, category: it.category };
    location.hash = 'search';
  }

  // ============ 视图：搜索 ============
  function viewSearch() {
    return `
    <div class="page-head">
      <div>
        <h2>搜索资源</h2>
        <div class="desc">从 6v520 全分类并发检索，勾选磁力链后一键推送到 2dland 离线下载</div>
      </div>
    </div>
    <div class="search-bar">
      <input id="kw" class="input" placeholder="输入电影 / 剧集名，如：灵魂伴侣" autocomplete="off">
      <button id="btnSearch" class="btn primary lg">🔍 搜索</button>
    </div>
    <div id="searchHint" class="muted text-sm mb-12"></div>
    <div id="results"></div>
    <div id="selFab"></div>`;
  }
  function bindSearch() {
    $('#btnSearch').onclick = doSearch;
    $('#kw').addEventListener('keydown', e => { if (e.key === 'Enter') doSearch(); });
    // 来自发现页的跳转：自动用卡片标题搜索并展开磁力链
    if (state.pendingHome) {
      const kw = $('#kw');
      if (kw) kw.value = state.pendingHome.title;
      doSearch();
    }
  }
  async function doSearch() {
    const q = $('#kw').value.trim();
    if (!q) return;
    const hint = $('#searchHint'), res = $('#results'), btn = $('#btnSearch');
    btn.disabled = true;
    hint.innerHTML = '<span class="spinner"></span> 搜索中（站内搜索，通常 1~2 秒）…';
    res.innerHTML = '';
    const { ok, data } = await api.get('/api/search?q=' + encodeURIComponent(q));
    btn.disabled = false;
    hint.textContent = '';
    if (!ok) { res.innerHTML = `<div class="empty"><div class="ico">⚠️</div>${esc(data.error || '搜索失败')}</div>`; return; }
    let results = data || [];
    // 发现页跳转：若搜索结果未包含卡片 URL，插入到头部以便展开磁力链
    const pending = state.pendingHome;
    state.pendingHome = null;
    if (pending && pending.url && !results.some(r => r.url === pending.url)) {
      results = [{ title: pending.title, url: pending.url, category: pending.category, date: '' }, ...results];
    }
    if (!results.length) { res.innerHTML = '<div class="empty"><div class="ico">🔍</div>未找到匹配资源</div>'; return; }
    state.lastResults = results;
    hint.textContent = `找到 ${results.length} 条结果，点击「查看磁力链」展开`;
    res.innerHTML = results.map((r, i) => `
      <div class="result">
        <div class="result-head">
          ${r.date ? `<span class="tag date">${esc(r.date)}</span>` : ''}
          <span class="tag cat">${esc(catName(r.category))}</span>
          <a class="title" href="${esc(r.url)}" target="_blank">${esc(r.title)}</a>
          <button class="btn sm ghost" data-mag="${i}">查看磁力链 ▾</button>
        </div>
        <div class="result-body" id="mags-${i}"></div>
      </div>`).join('');
    $$('[data-mag]').forEach(b => b.onclick = () => toggleMags(+b.dataset.mag));
    // 自动展开来自发现页的卡片磁力链
    if (pending && pending.url) {
      const idx = results.findIndex(r => r.url === pending.url);
      if (idx >= 0) toggleMags(idx);
    }
  }
  async function toggleMags(i) {
    const box = $('#mags-' + i), btn = $(`[data-mag="${i}"]`);
    if (box.innerHTML) { box.innerHTML = ''; btn.textContent = '查看磁力链 ▾'; return; }
    const r = state.lastResults[i];
    box.innerHTML = '<div class="muted text-sm"><span class="spinner"></span> 加载磁力链…</div>';
    btn.textContent = '收起 ▴';
    const { ok, data } = await api.get('/api/magnets?url=' + encodeURIComponent(r.url));
    if (!ok) { box.innerHTML = `<div class="err-text">${esc(data.error || '加载失败')}</div>`; return; }
    if (!data || !data.length) { box.innerHTML = '<div class="muted text-sm">该页未提取到磁力链</div>'; return; }
    box.innerHTML = data.map((m, idx) => {
      const checked = state.selected.has(m.magnet);
      return `<label class="mag ${checked ? 'checked' : ''}">
        <input type="checkbox" data-idx="${idx}" ${checked ? 'checked' : ''}>
        <div class="mag-info">
          <div class="mag-desc">${esc(m.desc || m.name)}</div>
          <div class="mag-hash">${esc(m.magnet.slice(0, 80))}…</div>
        </div>
      </label>`;
    }).join('');
    box.querySelectorAll('input[type=checkbox]').forEach((cb, idx) => {
      const m = data[idx];
      cb.onchange = () => {
        if (cb.checked) state.selected.set(m.magnet, { name: m.name, magnet: m.magnet, category: r.category, title: r.title });
        else state.selected.delete(m.magnet);
        cb.closest('.mag').classList.toggle('checked', cb.checked);
        renderSelected();
      };
    });
  }
  function renderSelected() {
    const fab = $('#selFab');
    if (!fab) return;
    if (!state.selected.size) { fab.innerHTML = ''; return; }
    fab.innerHTML = `<div class="fab"><span>已选 <span class="count">${state.selected.size}</span> 条</span><button class="btn primary sm" id="btnPush">推送到 2dland →</button></div>`;
    $('#btnPush').onclick = doPush;
  }
  async function doPush() {
    const items = Array.from(state.selected.values());
    const btn = $('#btnPush');
    btn.disabled = true; btn.textContent = '推送中…';
    const { ok, data } = await api.post('/api/push', { magnets: items });
    if (!ok) { toast(data.error || '推送失败', 'error'); btn.disabled = false; btn.textContent = '推送到 2dland →'; return; }
    showPushResult(data);
    state.selected.clear();
    $$('.mag.checked').forEach(el => el.classList.remove('checked'));
    $$('.mag input[type=checkbox]').forEach(cb => cb.checked = false);
    renderSelected();
  }
  function showPushResult(data) {
    const okN = data.items.filter(x => x.ok).length;
    const failN = data.items.length - okN;
    const root = $('#modal-root');
    root.innerHTML = `
    <div class="modal-mask" id="pushMask">
      <div class="modal">
        <div class="modal-head"><h3>推送结果</h3><button class="icon-btn" id="closePushX">✕</button></div>
        <div class="modal-body">
          <div class="row gap-sm mb-12">
            <span class="pill ok"><span class="dot"></span>${okN} 成功</span>
            ${failN > 0 ? `<span class="pill err"><span class="dot"></span>${failN} 失败</span>` : ''}
          </div>
          <div style="max-height:320px;overflow:auto">
            ${data.items.map(x => `
              <div class="task">
                <div class="t-name">
                  <div>${esc(x.folder)}${x.season ? ' / ' + esc(x.season) : ''}</div>
                  <div class="muted text-xs">${esc(x.name)}</div>
                  ${x.ok ? '' : `<div class="err-text text-xs">⚠ ${esc(x.error)}</div>`}
                </div>
                <span class="pill ${x.ok ? 'ok' : 'err'}">${x.ok ? '成功' : '失败'}</span>
              </div>`).join('')}
          </div>
        </div>
        <div class="modal-foot"><button class="btn primary" id="closePush">关闭</button></div>
      </div>
    </div>`;
    const close = () => root.innerHTML = '';
    $('#closePush').onclick = close;
    $('#closePushX').onclick = close;
    $('#pushMask').onclick = e => { if (e.target.id === 'pushMask') close(); };
  }

  // ============ 视图：任务 ============
  const TASK_PAGE_SIZE = 50;
  function viewTasks() {
    const arOn = localStorage.getItem('task_auto_refresh') === '1';
    const arSec = parseInt(localStorage.getItem('task_refresh_sec')) || 10;
    return `
    <div class="page-head">
      <div><h2>离线任务</h2><div class="desc">查看 2dland 全部离线下载任务；删除任务会同步到 2dland</div></div>
      <div class="row gap-sm">
        <label class="auto-refresh" title="开启后任务列表按设定秒数自动刷新">
          <input type="checkbox" id="autoRefresh" ${arOn ? 'checked' : ''}> 自动刷新
          <input type="number" id="refreshSec" class="input-num" value="${arSec}" min="3" max="3600"> 秒
        </label>
        <button class="btn danger" id="btnBatchDel" disabled>🗑 批量删除 <span id="selCount">0</span></button>
        <button class="btn" id="btnRefreshTasks">⟳ 刷新</button>
      </div>
    </div>
    <div class="card"><div id="tasksList" class="muted text-sm"><span class="spinner"></span> 加载中…</div></div>`;
  }
  function bindTasks() {
    $('#btnRefreshTasks').onclick = () => loadTasks();
    $('#btnBatchDel').onclick = batchDelete;
    const cb = $('#autoRefresh'), sec = $('#refreshSec');
    if (cb) cb.onchange = () => { localStorage.setItem('task_auto_refresh', cb.checked ? '1' : '0'); startTaskAutoRefresh(); };
    if (sec) sec.onchange = () => { const v = Math.max(3, parseInt(sec.value) || 10); sec.value = v; localStorage.setItem('task_refresh_sec', String(v)); startTaskAutoRefresh(); };
    startTaskAutoRefresh();
  }
  // 自动刷新：按设定秒数定时拉取任务（保留选中与页码，不闪屏）。
  function startTaskAutoRefresh() {
    stopTaskAutoRefresh();
    if (localStorage.getItem('task_auto_refresh') !== '1') return;
    const sec = Math.max(3, parseInt(localStorage.getItem('task_refresh_sec')) || 10);
    state.taskRefreshTimer = setInterval(() => loadTasks(true), sec * 1000);
  }
  function stopTaskAutoRefresh() {
    if (state.taskRefreshTimer) { clearInterval(state.taskRefreshTimer); state.taskRefreshTimer = null; }
  }
  async function loadTasks(keepSel) {
    const box = $('#tasksList');
    if (!box) return;
    if (!keepSel) {
      box.innerHTML = '<span class="spinner"></span> 加载中…';
      state.taskSel.clear();
      updateSelUI();
    }
    const { ok, data } = await api.get('/api/tasks');
    if (!ok) { if (!keepSel) box.innerHTML = `<div class="err-text">${esc(data.error || '加载失败')}</div>`; return; }
    state.taskAll = data || [];
    if (!keepSel) state.taskPage = 1;
    renderTaskPage();
  }
  function renderTaskPage() {
    const box = $('#tasksList');
    if (!box) return;
    const all = state.taskAll;
    const total = all.length;
    if (total === 0) { box.innerHTML = '<div class="empty"><div class="ico">📋</div>暂无离线任务</div>'; updateSelUI(); return; }
    const totalPages = Math.max(1, Math.ceil(total / TASK_PAGE_SIZE));
    if (state.taskPage > totalPages) state.taskPage = totalPages;
    if (state.taskPage < 1) state.taskPage = 1;
    const start = (state.taskPage - 1) * TASK_PAGE_SIZE;
    const items = all.slice(start, start + TASK_PAGE_SIZE);
    const head = `<div class="task task-bar">
        <input type="checkbox" id="checkAll" class="t-check" title="全选当前页">
        <div class="t-name muted text-sm">第 ${state.taskPage}/${totalPages} 页 · 本页 ${items.length} 项 · 共 ${total} 项</div>
      </div>`;
    box.innerHTML = head + items.map(t => {
      const pct = t.progress || 0;
      const sCls = t.status === 1000 ? 'ok' : (t.status === 3 ? 'err' : (t.status === 1 || t.status === 2 ? 'warn' : ''));
      const name = t.name || t.url || '未命名';
      const checked = state.taskSel.has(t.identity) ? 'checked' : '';
      return `<div class="task">
        <input type="checkbox" class="t-check t-item" data-id="${esc(t.identity)}" ${checked}>
        <div class="t-name">
          <div>${esc(name)}</div>
          ${t.save_path ? `<div class="muted text-xs" title="${esc(t.save_path)}">📁 ${esc(t.save_path)}</div>` : ''}
        </div>
        <div class="progress"><span style="width:${pct}%"></span></div>
        <div class="pct">${pct}%</div>
        <span class="pill ${sCls}">${esc(taskStatus(t.status))}</span>
        ${t.status === 1000 && t.save_path ? `<button class="icon-btn t-org" data-save="${esc(t.save_path)}" data-name="${esc(name)}" title="整理文件：删广告 + 规范命名">✨</button>` : ''}
        <button class="icon-btn t-del" data-id="${esc(t.identity)}" data-name="${esc(name)}" title="删除此任务">🗑</button>
      </div>`;
    }).join('') + `<div class="pager">
        <button class="btn sm" id="prevPage" ${state.taskPage <= 1 ? 'disabled' : ''}>‹ 上一页</button>
        <span class="page-info">第 <b>${state.taskPage}</b> / ${totalPages} 页</span>
        <button class="btn sm" id="nextPage" ${state.taskPage >= totalPages ? 'disabled' : ''}>下一页 ›</button>
      </div>`;
    // 单项复选框
    $$('.t-item').forEach(cb => cb.onchange = () => {
      if (cb.checked) state.taskSel.add(cb.dataset.id);
      else state.taskSel.delete(cb.dataset.id);
      updateSelUI();
      syncCheckAll();
    });
    // 全选（仅当前页）
    const ca = $('#checkAll');
    if (ca) ca.onchange = () => {
      if (ca.checked) $$('.t-item').forEach(cb => { cb.checked = true; state.taskSel.add(cb.dataset.id); });
      else $$('.t-item').forEach(cb => { cb.checked = false; state.taskSel.delete(cb.dataset.id); });
      updateSelUI();
    };
    // 单条删除
    $$('.t-del').forEach(b => b.onclick = () => deleteTask(b.dataset.id, b.dataset.name));
    // 单条整理（仅已完成任务）
    $$('.t-org').forEach(b => b.onclick = () => organizeTask(b.dataset.save, b.dataset.name));
    // 翻页
    const prev = $('#prevPage'), next = $('#nextPage');
    if (prev) prev.onclick = () => { if (state.taskPage > 1) { state.taskPage--; renderTaskPage(); } };
    if (next) next.onclick = () => { if (state.taskPage < totalPages) { state.taskPage++; renderTaskPage(); } };
    syncCheckAll();
  }
  function updateSelUI() {
    const n = state.taskSel.size;
    const btn = $('#btnBatchDel'), cnt = $('#selCount');
    if (btn) btn.disabled = n === 0;
    if (cnt) cnt.textContent = n;
  }
  function syncCheckAll() {
    const ca = $('#checkAll');
    if (!ca) return;
    const items = $$('.t-item');
    ca.checked = items.length > 0 && items.every(cb => cb.checked);
  }
  async function deleteTask(identity, name) {
    const r = await confirmDialog({
      title: '删除任务',
      message: `确定要删除任务「<b>${esc(name)}</b>」吗？`,
    });
    if (!r.ok) return;
    const { ok, data } = await api.post('/api/tasks/delete', { identities: [identity], delete_files: r.checked });
    if (!ok) { toast(data.error || '删除失败', 'error'); return; }
    toast(r.checked ? '已删除任务及文件' : '已删除任务', 'success');
    loadTasks();
  }
  // organizeTask 整理已完成任务的下载文件：删除广告 + 规范化视频文件名，结果用弹窗展示。
  async function organizeTask(savePath, name) {
    const r = await confirmDialog({
      title: '整理文件',
      message: `整理任务「<b>${esc(name)}</b>」的下载文件？<br><span class="muted text-xs">将删除非视频广告文件，并把视频重命名为规范格式（电影：<标题> (<年份>)；剧集：标题S01E05）</span>`,
      confirmText: '整理',
      danger: false,
      checkboxLabel: '',
    });
    if (!r.ok) return;
    toast('正在整理…', 'info');
    const { ok, data } = await api.post('/api/tasks/organize', { save_path: savePath });
    if (!ok) { toast(data.error || '整理失败', 'error'); return; }
    showOrganizeResult(data, name);
  }
  function showOrganizeResult(res, name) {
    const root = $('#modal-root');
    const del = (res.deleted || []).map(n => `<li>🗑 ${esc(n)}</li>`).join('');
    const rn = (res.renamed || []).map(r => `<li>✏️ <s>${esc(r.old)}</s> → <b>${esc(r.new)}</b></li>`).join('');
    const sk = (res.skipped || []).map(n => `<li>⏭️ ${esc(n)}</li>`).join('');
    const empty = (!del && !rn && !sk) ? '<div class="muted">该目录下没有需要整理的文件（可能已整理过或无视频文件）</div>' : '';
    root.innerHTML = `
    <div class="modal-mask" id="orgMask">
      <div class="modal">
        <div class="modal-head"><h3>✨ 整理结果</h3><button class="icon-btn" id="orgX">✕</button></div>
        <div class="modal-body">
          <div class="text-sm mb-8">任务：<b>${esc(name)}</b></div>
          <div class="text-xs muted mb-8">📁 ${esc(res.save_path || '')}</div>
          ${empty}
          ${del ? `<div class="mt-8 text-sm"><b>删除广告/杂项（${res.deleted.length}）</b></div><ul class="org-list">${del}</ul>` : ''}
          ${rn ? `<div class="mt-8 text-sm"><b>重命名视频（${res.renamed.length}）</b></div><ul class="org-list">${rn}</ul>` : ''}
          ${sk ? `<div class="mt-8 text-sm"><b>跳过（${res.skipped.length}）</b><div class="muted text-xs">剧集文件解析不到集数时会跳过，保留原名</div></div><ul class="org-list">${sk}</ul>` : ''}
        </div>
        <div class="modal-foot">
          <button class="btn" id="orgClose">关闭</button>
        </div>
      </div>
    </div>`;
    const close = () => { root.innerHTML = ''; };
    $('#orgClose').onclick = close;
    $('#orgX').onclick = close;
    $('#orgMask').onclick = e => { if (e.target.id === 'orgMask') close(); };
  }
  async function batchDelete() {
    const ids = Array.from(state.taskSel);
    if (!ids.length) return;
    const r = await confirmDialog({
      title: '批量删除任务',
      message: `确定要删除选中的 <b>${ids.length}</b> 个任务吗？（跨页选中也会一并删除）`,
    });
    if (!r.ok) return;
    const btn = $('#btnBatchDel');
    const old = btn.innerHTML;
    btn.disabled = true; btn.textContent = '删除中…';
    const { ok, data } = await api.post('/api/tasks/delete', { identities: ids, delete_files: r.checked });
    btn.disabled = false; btn.innerHTML = old;
    if (!ok) { toast(data.error || '删除失败', 'error'); return; }
    toast(`已删除 ${ids.length} 个任务`, 'success');
    loadTasks();
  }
  // confirmDialog 通用确认弹窗，返回 { ok, checked }。
  function confirmDialog({ title, message, confirmText = '删除', danger = true, checkboxLabel = '同时删除已下载的文件' }) {
    return new Promise(resolve => {
      const root = $('#modal-root');
      root.innerHTML = `
      <div class="modal-mask" id="confirmMask">
        <div class="modal">
          <div class="modal-head"><h3>${esc(title)}</h3><button class="icon-btn" id="confirmX">✕</button></div>
          <div class="modal-body">
            <div>${message}</div>
            ${checkboxLabel ? `<label class="checkbox mt-12"><input type="checkbox" id="confirmCb"> ${esc(checkboxLabel)}</label>` : ''}
          </div>
          <div class="modal-foot">
            <button class="btn" id="confirmCancel">取消</button>
            <button class="btn ${danger ? 'danger' : 'primary'}" id="confirmOk">${esc(confirmText)}</button>
          </div>
        </div>
      </div>`;
      const close = res => { root.innerHTML = ''; resolve(res); };
      $('#confirmOk').onclick = () => close({ ok: true, checked: $('#confirmCb') ? $('#confirmCb').checked : false });
      $('#confirmCancel').onclick = () => close({ ok: false });
      $('#confirmX').onclick = () => close({ ok: false });
      $('#confirmMask').onclick = e => { if (e.target.id === 'confirmMask') close({ ok: false }); };
    });
  }

  // ============ 视图：文件管理 ============
  function viewFiles() {
    return `
    <div class="page-head">
      <div><h2>文件管理</h2><div class="desc">直接管理 2dland 网盘文件，可拖拽文件到文件夹移动</div></div>
      <div class="row gap-sm">
        <button class="btn" id="btnNewFolder">📁 新建文件夹</button>
        <button class="btn danger" id="btnFileDel" disabled>🗑 删除选中 <span id="fileSelCount"></span></button>
        <button class="btn" id="btnRefreshFiles">⟳ 刷新</button>
      </div>
    </div>
    <div class="card">
      <div class="breadcrumb" id="breadcrumb"></div>
      <div id="fileList" class="muted text-sm"><span class="spinner"></span> 加载中…</div>
      <div class="help mt-12">💡 拖拽文件/文件夹到目标文件夹或面包屑路径上即可移动；点击文件夹名进入；删除是移到回收站，可在 2dland 恢复。</div>
    </div>`;
  }
  function bindFiles() {
    $('#btnRefreshFiles').onclick = loadFiles;
    $('#btnNewFolder').onclick = newFolder;
    $('#btnFileDel').onclick = deleteFiles;
  }
  async function loadFiles() {
    const box = $('#fileList');
    if (!box) return;
    box.innerHTML = '<span class="spinner"></span> 加载中…';
    state.fileSel.clear();
    updateFileSelUI();
    renderBreadcrumb();
    const { ok, data } = await api.get('/api/files?path=' + encodeURIComponent(state.fileCwd));
    if (!ok) { box.innerHTML = `<div class="err-text">${esc(data.error || '加载失败')}</div>`; return; }
    state.fileItems = data || [];
    renderFileList();
  }
  function renderBreadcrumb() {
    const bc = $('#breadcrumb');
    if (!bc) return;
    const cwd = state.fileCwd || '/';
    const segs = cwd.split('/').filter(Boolean);
    let html = `<span class="crumb drop-target" data-path="/" title="回到根目录">🏠 根目录</span>`;
    let cur = '';
    segs.forEach(seg => {
      cur += '/' + seg;
      html += `<span class="crumb-sep">/</span><span class="crumb drop-target" data-path="${esc(cur)}">${esc(seg)}</span>`;
    });
    bc.innerHTML = html;
    $$('#breadcrumb .drop-target').forEach(el => {
      el.onclick = () => { if (el.dataset.path !== state.fileCwd) { state.fileCwd = el.dataset.path; loadFiles(); } };
      el.addEventListener('dragover', e => { e.preventDefault(); el.classList.add('drag-over'); e.dataTransfer.dropEffect = 'move'; });
      el.addEventListener('dragleave', () => el.classList.remove('drag-over'));
      el.addEventListener('drop', e => {
        e.preventDefault();
        el.classList.remove('drag-over');
        const ids = (e.dataTransfer.getData('text/plain') || '').split(',').filter(Boolean);
        if (ids.length && el.dataset.path !== state.fileCwd) moveFiles(ids, el.dataset.path, el.textContent.trim());
      });
    });
  }
  function renderFileList() {
    const box = $('#fileList');
    if (!box) return;
    const items = state.fileItems.slice().sort((a, b) => (b.dir - a.dir) || a.name.localeCompare(b.name, 'zh'));
    if (!items.length) { box.innerHTML = '<div class="empty"><div class="ico">📂</div>空文件夹</div>'; return; }
    box.innerHTML = items.map(f => {
      const icon = f.dir ? '📁' : iconForFile(f.name);
      const meta = f.dir ? `${f.dirs} 文件夹 · ${f.files} 文件` : formatSize(f.size);
      const date = f.update_ts ? new Date(f.update_ts * 1000).toLocaleDateString('zh-CN') : '';
      const checked = state.fileSel.has(f.identity) ? 'checked' : '';
      return `<div class="fitem ${f.dir ? 'is-dir drop-target' : ''}" draggable="true" data-id="${esc(f.identity)}" data-name="${esc(f.name)}" data-path="${esc(f.path)}" data-dir="${f.dir}">
        <input type="checkbox" class="f-check" data-id="${esc(f.identity)}" ${checked}>
        <span class="f-icon">${icon}</span>
        <span class="f-name" title="${esc(f.name)}">${esc(f.name)}</span>
        <span class="f-meta muted text-xs">${esc(meta)}</span>
        <span class="f-date muted text-xs">${esc(date)}</span>
        <button class="icon-btn f-rename" data-id="${esc(f.identity)}" data-name="${esc(f.name)}" title="重命名">✏️</button>
      </div>`;
    }).join('');
    bindFileEvents();
  }
  let _dragIds = [];
  function bindFileEvents() {
    $$('.f-check').forEach(cb => cb.onchange = () => {
      if (cb.checked) state.fileSel.add(cb.dataset.id);
      else state.fileSel.delete(cb.dataset.id);
      updateFileSelUI();
    });
    $$('.fitem').forEach(el => {
      if (el.dataset.dir === 'true') {
        el.querySelector('.f-name').onclick = () => { state.fileCwd = el.dataset.path; loadFiles(); };
      }
      el.addEventListener('dragstart', e => {
        const id = el.dataset.id;
        _dragIds = state.fileSel.has(id) ? Array.from(state.fileSel) : [id];
        e.dataTransfer.effectAllowed = 'move';
        e.dataTransfer.setData('text/plain', _dragIds.join(','));
        el.classList.add('dragging');
      });
      el.addEventListener('dragend', () => { el.classList.remove('dragging'); _dragIds = []; });
    });
    $$('.fitem.is-dir').forEach(el => {
      el.addEventListener('dragover', e => { e.preventDefault(); el.classList.add('drag-over'); e.dataTransfer.dropEffect = 'move'; });
      el.addEventListener('dragleave', () => el.classList.remove('drag-over'));
      el.addEventListener('drop', e => {
        e.preventDefault();
        el.classList.remove('drag-over');
        const ids = (e.dataTransfer.getData('text/plain') || '').split(',').filter(Boolean);
        if (ids.length) moveFiles(ids, el.dataset.path, el.dataset.name);
      });
    });
    $$('.f-rename').forEach(b => b.onclick = e => { e.stopPropagation(); renameFile(b.dataset.id, b.dataset.name); });
  }
  function updateFileSelUI() {
    const n = state.fileSel.size;
    const btn = $('#btnFileDel'), cnt = $('#fileSelCount');
    if (btn) btn.disabled = n === 0;
    if (cnt) cnt.textContent = n > 0 ? `(${n})` : '';
  }
  async function newFolder() {
    const name = await promptDialog({ title: '新建文件夹', placeholder: '输入文件夹名' });
    if (!name) return;
    const { ok, data } = await api.post('/api/files/mkdir', { parent: state.fileCwd, name });
    if (!ok) { toast(data.error || '创建失败', 'error'); return; }
    toast('文件夹已创建', 'success');
    loadFiles();
  }
  async function renameFile(identity, oldName) {
    const name = await promptDialog({ title: '重命名', value: oldName, placeholder: '输入新名称' });
    if (!name || name === oldName) return;
    const { ok, data } = await api.post('/api/files/rename', { identity, name });
    if (!ok) { toast(data.error || '重命名失败', 'error'); return; }
    toast('已重命名', 'success');
    loadFiles();
  }
  async function deleteFiles() {
    const ids = Array.from(state.fileSel);
    if (!ids.length) return;
    const r = await confirmDialog({
      title: '删除文件',
      message: `确定要删除选中的 <b>${ids.length}</b> 项吗？（移到回收站，可在 2dland 恢复）`,
      checkboxLabel: '',
    });
    if (!r.ok) return;
    const { ok, data } = await api.post('/api/files/delete', { identities: ids });
    if (!ok) { toast(data.error || '删除失败', 'error'); return; }
    toast(`已删除 ${ids.length} 项`, 'success');
    loadFiles();
  }
  async function moveFiles(ids, dest, destName) {
    const r = await confirmDialog({
      title: '移动文件',
      message: `确定要把 <b>${ids.length}</b> 项移动到「<b>${esc(destName)}</b>」吗？`,
      confirmText: '移动', danger: false, checkboxLabel: '',
    });
    if (!r.ok) return;
    const { ok, data } = await api.post('/api/files/move', { identities: ids, dest });
    if (!ok) { toast(data.error || '移动失败', 'error'); return; }
    toast(`已移动 ${ids.length} 项`, 'success');
    loadFiles();
  }
  function formatSize(b) {
    if (!b) return '-';
    const u = ['B', 'KB', 'MB', 'GB', 'TB'];
    let i = 0, n = b;
    while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
    return (n < 10 && i > 0 ? n.toFixed(1) : Math.round(n)) + ' ' + u[i];
  }
  function iconForFile(name) {
    const ext = (name.split('.').pop() || '').toLowerCase();
    if (['mp4', 'mkv', 'avi', 'rmvb', 'mov', 'wmv', 'flv', 'ts', 'm4v'].includes(ext)) return '🎬';
    if (['mp3', 'flac', 'ape', 'wav', 'm4a', 'aac'].includes(ext)) return '🎵';
    if (['jpg', 'jpeg', 'png', 'gif', 'bmp', 'webp'].includes(ext)) return '🖼️';
    if (['srt', 'ass', 'ssa', 'sub', 'vtt'].includes(ext)) return '📝';
    if (['zip', 'rar', '7z', 'tar', 'gz'].includes(ext)) return '📦';
    return '📄';
  }
  // promptDialog 输入弹窗，返回输入文本或 null。
  function promptDialog({ title, value = '', placeholder = '' }) {
    return new Promise(resolve => {
      const root = $('#modal-root');
      root.innerHTML = `
      <div class="modal-mask" id="promptMask">
        <div class="modal">
          <div class="modal-head"><h3>${esc(title)}</h3><button class="icon-btn" id="promptX">✕</button></div>
          <div class="modal-body">
            <input id="promptInput" class="input" value="${esc(value)}" placeholder="${esc(placeholder)}">
          </div>
          <div class="modal-foot">
            <button class="btn" id="promptCancel">取消</button>
            <button class="btn primary" id="promptOk">确定</button>
          </div>
        </div>
      </div>`;
      const inp = $('#promptInput');
      inp.focus(); inp.select();
      const close = res => { root.innerHTML = ''; resolve(res); };
      const ok = () => { const v = inp.value.trim(); close(v || null); };
      $('#promptOk').onclick = ok;
      $('#promptCancel').onclick = () => close(null);
      $('#promptX').onclick = () => close(null);
      inp.addEventListener('keydown', e => { if (e.key === 'Enter') ok(); if (e.key === 'Escape') close(null); });
      $('#promptMask').onclick = e => { if (e.target.id === 'promptMask') close(null); };
    });
  }

  // ============ 视图：设置 ============
  function viewSettingsLoading() {
    return `<div class="card"><span class="spinner"></span> 加载设置中…</div>`;
  }
  function viewSettings() {
    const s = state.settings;
    if (!s) return viewSettingsLoading();
    return `
    <div class="page-head">
      <div><h2>设置</h2><div class="desc">所有配置均在此处管理，无需手动编辑 config 文件</div></div>
    </div>

    <div class="card">
      <h3>🔐 访问安全</h3>
      <div class="sub">${s.has_access_password ? '已设置访问密码' : '尚未设置访问密码（建议设置）'}</div>
      <div class="field">
        <label>新访问密码</label>
        <input type="password" id="access_password" class="input" placeholder="${s.has_access_password ? '留空则不修改' : '设置访问密码'}" autocomplete="new-password">
        <div class="help">修改后需用新密码重新登录本控制台。</div>
      </div>
    </div>

    <div class="card">
      <h3>🌐 2dland 连接</h3>
      <div class="sub">配置开放平台凭证后，可在下方直接发起设备码登录。</div>
      <div class="field-row">
        <div class="field"><label>Client ID</label><input id="client_id" class="input" value="${esc(s.client_id || '')}"></div>
        <div class="field"><label>Client Secret</label><input type="password" id="client_secret" class="input" value="${esc(s.client_secret || '')}" placeholder="修改凭证时填写"></div>
      </div>
      <div id="login2dland"></div>
    </div>

    <div class="card">
      <h3>🎬 TMDB 标题规范化</h3>
      <div class="sub">借助 TMDB 获取标准片名与年份，构建规范的目录结构。TMDB 在国内通常需通过代理访问。</div>
      <div class="field-row">
        <div class="field"><label>API Key</label><input id="tmdb_api_key" class="input" value="${esc(s.tmdb_api_key || '')}" placeholder="themoviedb.org 申请"></div>
        <div class="field"><label>语言</label><input id="tmdb_language" class="input" value="${esc(s.tmdb_language || 'zh-CN')}"></div>
      </div>
      <div class="field"><label>代理服务器</label><input id="tmdb_proxy" class="input" value="${esc(s.tmdb_proxy || '')}" placeholder="如 http://127.0.0.1:7890"></div>
      <div class="row">
        <button class="btn" id="btnTestTmdb">🔌 测试连接</button>
        <span id="tmdbTestResult" class="text-sm muted"></span>
      </div>
    </div>

    <div class="card">
      <h3>📁 下载与目录</h3>
      <div class="field-row">
        <div class="field"><label>2dland 根目录名</label><input id="base_dir" class="input" value="${esc(s.base_dir || '6v下载')}"></div>
        <div class="field"><label>每分类最大翻页数（备用）</label><input type="number" id="max_pages" class="input" value="${esc(s.max_pages || 8)}" min="1" max="50"></div>
      </div>
      <div class="help">⚠️ 每分类最大翻页数：仅当站内搜索无结果、回退到列表页爬取时生效。正常搜索用不到此选项，保持默认 8 即可。</div>
      <div class="help">目录结构：电影 = 根目录 / 分类 / 标题(年份)；剧集 = 根目录 / 分类 / 标题(年份) / 第N季。</div>
    </div>

    <div class="row end">
      <button class="btn primary lg" id="btnSaveSettings">💾 保存设置</button>
    </div>`;
  }
  function bindSettings() {
    render2dlandLogin();
    $('#btnSaveSettings').onclick = saveSettings;
    $('#btnTestTmdb').onclick = testTmdb;
  }
  function render2dlandLogin() {
    const box = $('#login2dland');
    if (!box || !state.settings) return;
    const s = state.settings;
    if (s.logged_in_2dland) {
      box.innerHTML = `
      <div class="login-card">
        <div class="row between">
          <span class="pill ok"><span class="dot"></span>2dland 已登录</span>
          <button class="btn sm danger" id="btn2dlandLogout">退出 2dland 登录</button>
        </div>
      </div>`;
      $('#btn2dlandLogout').onclick = async () => {
        const { ok, data } = await api.post('/api/auth/logout');
        if (!ok) { toast(data.error || '退出失败', 'error'); return; }
        toast('已退出 2dland 登录', 'success');
        await refreshAuth();
        await loadSettings();
      };
    } else if (s.has_credentials) {
      box.innerHTML = `
      <div class="login-card">
        <div class="step"><span class="num">1</span><div>点击下方按钮发起设备码授权</div></div>
        <button class="btn primary" id="btn2dlandLogin">🔑 登录 2dland</button>
      </div>`;
      $('#btn2dlandLogin').onclick = start2dlandLogin;
    } else {
      box.innerHTML = `<div class="login-card muted text-sm">请先填写并保存 Client ID / Client Secret，再进行 2dland 登录。</div>`;
    }
  }
  async function start2dlandLogin() {
    const btn = $('#btn2dlandLogin');
    if (btn) btn.disabled = true;
    const { ok, data } = await api.post('/api/auth/login');
    if (!ok) { toast(data.error || '发起登录失败', 'error'); if (btn) btn.disabled = false; return; }
    showLoginModal(data);
  }
  function showLoginModal(data) {
    const root = $('#modal-root');
    root.innerHTML = `
    <div class="modal-mask" id="loginMask">
      <div class="modal">
        <div class="modal-head"><h3>登录 2dland</h3><button class="icon-btn" id="closeLoginX">✕</button></div>
        <div class="modal-body">
          <div class="step"><span class="num">1</span><div>在浏览器中打开下方地址：</div></div>
          <div class="link-box"><a href="${esc(data.verification_uri)}" target="_blank">${esc(data.verification_uri)}</a><button class="btn sm ghost" id="copyLink">复制</button></div>
          <div class="step mt-12"><span class="num">2</span><div>输入以下用户码（点击复制）：</div></div>
          <div class="code-box" id="userCode" title="点击复制">${esc(data.user_code)}</div>
          <div class="step mt-12"><span class="num">3</span><div id="loginState" class="muted text-sm">等待授权完成…</div></div>
        </div>
        <div class="modal-foot"><button class="btn" id="cancelLogin">关闭</button></div>
      </div>
    </div>`;
    const close = () => { if (state.pollTimer) { clearInterval(state.pollTimer); state.pollTimer = null; } root.innerHTML = ''; };
    $('#closeLoginX').onclick = close;
    $('#cancelLogin').onclick = close;
    $('#copyLink').onclick = () => copy(data.verification_uri);
    $('#userCode').onclick = () => copy(data.user_code);
    const interval = (data.interval || 5) * 1000;
    state.pollTimer = setInterval(async () => {
      const { ok, data: d } = await api.get('/api/auth/poll');
      if (!ok) return;
      const st = $('#loginState');
      if (st) st.textContent = '状态：' + d.status + '…';
      if (d.logged_in) {
        close();
        toast('2dland 登录成功！', 'success');
        await refreshAuth();
        await loadSettings();
      }
    }, interval);
  }
  async function saveSettings() {
    const body = {
      access_password: $('#access_password').value,
      client_id: $('#client_id').value.trim(),
      client_secret: $('#client_secret').value.trim(),
      tmdb_api_key: $('#tmdb_api_key').value.trim(),
      tmdb_proxy: $('#tmdb_proxy').value.trim(),
      tmdb_language: $('#tmdb_language').value.trim() || 'zh-CN',
      max_pages: parseInt($('#max_pages').value) || 0,
      base_dir: $('#base_dir').value.trim(),
    };
    const btn = $('#btnSaveSettings');
    btn.disabled = true; btn.textContent = '保存中…';
    const { ok, data } = await api.post('/api/settings', body);
    btn.disabled = false; btn.textContent = '💾 保存设置';
    if (!ok) { toast(data.error || '保存失败', 'error'); return; }
    toast('设置已保存', 'success');
    if (data.relogin_needed) toast('2dland 凭证已变更，请重新登录 2dland', 'warn', 5000);
    await refreshAuth();
    await loadSettings();
    if (body.access_password) {
      state.uiSession.logged_in = false;
      render();
    }
  }
  async function testTmdb() {
    const body = {
      tmdb_api_key: $('#tmdb_api_key').value.trim(),
      tmdb_proxy: $('#tmdb_proxy').value.trim(),
      tmdb_language: $('#tmdb_language').value.trim(),
    };
    const btn = $('#btnTestTmdb'), res = $('#tmdbTestResult');
    btn.disabled = true;
    res.innerHTML = '<span class="spinner"></span> 测试中…';
    const { ok, data } = await api.post('/api/settings/test', body);
    btn.disabled = false;
    if (!ok) { res.innerHTML = `<span class="err-text">✗ ${esc(data.error || '测试失败')}</span>`; return; }
    if (data.ok) {
      res.innerHTML = `<span class="ok-text">✓ 连接成功${data.title ? '，命中：' + esc(data.title) : ''}（${data.duration_ms}ms）</span>`;
    } else {
      res.innerHTML = `<span class="err-text">✗ 失败：${esc(data.error || '未知错误')}</span>`;
    }
  }
  async function loadSettings() {
    const { ok, data } = await api.get('/api/settings');
    if (!ok) return;
    state.settings = data;
    if (state.view === 'settings') {
      $('#content').innerHTML = viewSettings();
      bindSettings();
    }
  }

  // ============ 公共：刷新 2dland 登录状态 ============
  async function refreshAuth() {
    const { ok, data } = await api.get('/api/auth/status');
    if (ok) state.auth = data;
  }

  // ============ 启动 ============
  init();
})();
