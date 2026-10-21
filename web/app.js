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
    homeCats: [],           // 发现页各栏 [{category,name,items}]：最新电影/最新电视剧 + 11 分类
    homeActiveCat: null,    // 当前选中的栏 id
    homePicked: new Map(),  // url -> {title,url,category} 发现页勾选的条目
    magCache: new Map(),    // url -> magnets[] 详情页磁力缓存
    pendingHome: null,      // 从发现页点击跳转到搜索页时携带的 {title,url,category}
    theme: 'auto',
    pollTimer: null,
    taskRefreshTimer: null,  // 任务页自动刷新定时器
    homeRefreshTimer: null,  // 发现页自动刷新定时器
    orgLogs: [],           // 整理记录（最新在前）
    orgLogsTotal: 0,       // 服务端总条数
    orgLogsFilter: '',     // 整理记录筛选词
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
    state.view = ['home', 'search', 'tasks', 'organize-logs', 'follows', 'files', 'settings'].includes(h) ? h : 'home';
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
    if (state.view !== 'home') stopHomeAutoRefresh();
    if (state.view === 'home') {
      c.innerHTML = viewHome(); bindHome();
      // 不自动刷新：有缓存直接显示上次内容，无缓存显示引导提示等用户点刷新
      if (state.homeCats.length) { renderHomeTabs(); renderHomeGrid(); }
    }
    else if (state.view === 'tasks') { c.innerHTML = viewTasks(); bindTasks(); loadTasks(); }
    else if (state.view === 'organize-logs') { c.innerHTML = viewOrganizeLogs(); bindOrganizeLogs(); loadOrganizeLogs(); }
    else if (state.view === 'follows') { c.innerHTML = viewFollows(); bindFollows(); loadFollows(); }
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
          <a class="nav-item ${state.view === 'organize-logs' ? 'active' : ''}" href="#organize-logs">📝 整理记录</a>
          <a class="nav-item ${state.view === 'follows' ? 'active' : ''}" href="#follows">⭐ 关注</a>
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
        <a class="m-nav-item ${state.view === 'organize-logs' ? 'active' : ''}" href="#organize-logs"><span class="m-ico">📝</span><span class="m-lbl">整理记录</span></a>
        <a class="m-nav-item ${state.view === 'follows' ? 'active' : ''}" href="#follows"><span class="m-ico">⭐</span><span class="m-lbl">关注</span></a>
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
      stopHomeAutoRefresh();
      render();
    };
  }

  // ============ 视图：发现（分类标签 + 栏内按发布日） ============
  // 数据源：最新电影/最新电视剧整页 + 11 分类近 10 天。刷新：进页不自动刷。
  function viewHome() {
    const arOn = localStorage.getItem('home_auto_refresh') === '1';
    const arMin = parseInt(localStorage.getItem('home_refresh_min')) || 5;
    return `
    <div class="page-head">
      <div><h2>发现</h2><div class="desc">勾选条目前的方框，点「批量下载」选磁力再推送；点标题仍可进详情</div></div>
      <div class="row gap-sm">
        <label class="auto-refresh" title="全选当前分类">
          <input type="checkbox" id="homeSelectAll"> 全选本栏
        </label>
        <button class="btn primary" id="btnHomeBatch" disabled>⬇ 批量下载</button>
        <label class="auto-refresh" title="开启后按设定分钟数自动刷新发现页">
          <input type="checkbox" id="homeAutoRefresh" ${arOn ? 'checked' : ''}> 定时刷新
          <input type="number" id="homeRefreshMin" class="input-num" value="${arMin}" min="1" max="1440"> 分钟
        </label>
        <button class="btn" id="btnRefreshHome">⟳ 刷新</button>
      </div>
    </div>
    <div id="homeTabs" class="home-tabs"></div>
    <div id="homeGrid" class="home-list"><div class="empty"><div class="ico">📥</div>点击右上角「⟳ 刷新」加载最新资源</div></div>
    <div id="homeFab"></div>`;
  }
  function bindHome() {
    const btn = $('#btnRefreshHome');
    if (btn) btn.onclick = () => loadHome();
    const cb = $('#homeAutoRefresh'), min = $('#homeRefreshMin');
    if (cb) cb.onchange = () => { localStorage.setItem('home_auto_refresh', cb.checked ? '1' : '0'); startHomeAutoRefresh(); };
    if (min) min.onchange = () => { const v = Math.max(1, parseInt(min.value) || 5); min.value = v; localStorage.setItem('home_refresh_min', String(v)); startHomeAutoRefresh(); };
    const all = $('#homeSelectAll');
    if (all) all.onchange = () => toggleHomeSelectAll(all.checked);
    const batch = $('#btnHomeBatch');
    if (batch) batch.onclick = openHomeBatchModal;
    startHomeAutoRefresh();
    renderHomePickBar();
  }
  function startHomeAutoRefresh() {
    stopHomeAutoRefresh();
    if (localStorage.getItem('home_auto_refresh') !== '1') return;
    const min = Math.max(1, parseInt(localStorage.getItem('home_refresh_min')) || 5);
    state.homeRefreshTimer = setInterval(() => loadHome(true), min * 60 * 1000);
  }
  function stopHomeAutoRefresh() {
    if (state.homeRefreshTimer) { clearInterval(state.homeRefreshTimer); state.homeRefreshTimer = null; }
  }
  async function loadHome(silent) {
    const grid = $('#homeGrid'), tabs = $('#homeTabs');
    if (!silent) {
      if (grid) grid.innerHTML = '<div class="loading-box"><span class="spinner"></span> 正在抓取最新页 + 11 分类近 10 天…</div>';
      if (tabs) tabs.innerHTML = '';
    }
    const { ok, data } = await api.get('/api/home');
    if (!ok) { if (!silent && grid) grid.innerHTML = `<div class="empty"><div class="ico">⚠️</div>${esc(data.error || '加载失败')}</div>`; return; }
    state.homeCats = (data && data.cats) || [];
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
      `<button class="home-tab ${c.category === state.homeActiveCat ? 'active' : ''}" data-cat="${esc(c.category)}">${esc(c.name)}<span class="home-tab-count">${(c.items || []).length}</span></button>`
    ).join('');
    $$('.home-tab').forEach(b => b.onclick = () => {
      if (state.homeActiveCat === b.dataset.cat) return;
      state.homeActiveCat = b.dataset.cat;
      renderHomeTabs();
      renderHomeGrid();
    });
  }
  function renderHomeGrid() {
    const grid = $('#homeGrid');
    if (!grid) return;
    const cat = state.homeCats.find(c => c.category === state.homeActiveCat);
    if (!cat) { grid.innerHTML = ''; return; }
    const items = cat.items || [];
    if (!items.length) { grid.innerHTML = '<div class="empty"><div class="ico">📭</div>该分类暂无内容</div>'; return; }
    grid.innerHTML = items.map((it, i) => {
      const d = it.date ? it.date.slice(5) : '';
      const checked = state.homePicked.has(it.url);
      return `<div class="home-row ${checked ? 'picked' : ''}" data-idx="${i}" title="${esc(it.title)}">
        <input type="checkbox" class="home-check" data-idx="${i}" ${checked ? 'checked' : ''} aria-label="选择 ${esc(it.title)}">
        ${d ? `<span class="home-date">${esc(d)}</span>` : ''}
        <span class="home-text">${esc(it.title)}</span>
        <button class="btn sm ghost home-sub-btn" data-home-sub="${i}" title="添加关注">⭐</button>
      </div>`;
    }).join('');
    $$('.home-check').forEach(el => el.onclick = e => {
      e.stopPropagation();
      toggleHomePick(+el.dataset.idx, el.checked);
    });
    $$('[data-home-sub]').forEach(b => b.onclick = async e => {
      e.stopPropagation();
      const it = currentHomeItems()[+b.dataset.homeSub];
      if (!it) return;
      addFollowDialog({ keyword: it.title, sixv_url: it.url, category: it.category });
    });
    $$('.home-row').forEach(el => el.onclick = e => {
      if (e.target.classList.contains('home-check')) return;
      clickHome(+el.dataset.idx);
    });
    syncHomeSelectAll();
    renderHomePickBar();
  }
  function currentHomeItems() {
    const cat = state.homeCats.find(c => c.category === state.homeActiveCat);
    return (cat && cat.items) || [];
  }
  function toggleHomePick(i, on) {
    const it = currentHomeItems()[i];
    if (!it || !it.url) return;
    if (on) state.homePicked.set(it.url, { title: it.title, url: it.url, category: it.category });
    else state.homePicked.delete(it.url);
    const row = $(`.home-row[data-idx="${i}"]`);
    if (row) row.classList.toggle('picked', on);
    syncHomeSelectAll();
    renderHomePickBar();
  }
  function toggleHomeSelectAll(on) {
    currentHomeItems().forEach(it => {
      if (!it.url) return;
      if (on) state.homePicked.set(it.url, { title: it.title, url: it.url, category: it.category });
      else state.homePicked.delete(it.url);
    });
    renderHomeGrid();
  }
  function syncHomeSelectAll() {
    const all = $('#homeSelectAll');
    if (!all) return;
    const items = currentHomeItems();
    all.checked = items.length > 0 && items.every(it => state.homePicked.has(it.url));
    all.indeterminate = !all.checked && items.some(it => state.homePicked.has(it.url));
  }
  function renderHomePickBar() {
    const n = state.homePicked.size;
    const btn = $('#btnHomeBatch');
    if (btn) {
      btn.disabled = n === 0;
      btn.textContent = n ? `⬇ 批量下载 (${n})` : '⬇ 批量下载';
    }
    const fab = $('#homeFab');
    if (!fab) return;
    if (!n) { fab.innerHTML = ''; return; }
    fab.innerHTML = `<div class="fab"><span>已选 <span class="count">${n}</span> 部</span><button class="btn primary sm" id="btnHomeFabPush">选择磁力并推送 →</button></div>`;
    const b = $('#btnHomeFabPush');
    if (b) b.onclick = openHomeBatchModal;
  }
  async function fetchMagnetsCached(url) {
    if (state.magCache.has(url)) return { ok: true, data: state.magCache.get(url) };
    const res = await api.get('/api/magnets?url=' + encodeURIComponent(url));
    if (res.ok && Array.isArray(res.data)) state.magCache.set(url, res.data);
    return res;
  }
  async function openHomeBatchModal() {
    const picked = Array.from(state.homePicked.values());
    if (!picked.length) { toast('请先勾选要下载的条目', 'error'); return; }
    const root = $('#modal-root');
    if (!root) return;
    root.innerHTML = `
    <div class="modal-mask" id="homeBatchMask">
      <div class="modal wide">
        <div class="modal-head"><h3>选择磁力链（${picked.length} 部）</h3><button class="icon-btn" id="closeHomeBatchX">✕</button></div>
        <div class="modal-body" id="homeBatchBody"><div class="loading-box"><span class="spinner"></span> 正在拉取磁力链…</div></div>
        <div class="modal-foot">
          <label class="auto-refresh"><input type="checkbox" id="homeBatchAll"> 全选磁力</label>
          <button class="btn" id="cancelHomeBatch">取消</button>
          <button class="btn primary" id="btnHomeBatchPush" disabled>推送到 2dland</button>
        </div>
      </div>
    </div>`;
    const close = () => { root.innerHTML = ''; };
    $('#closeHomeBatchX').onclick = close;
    $('#cancelHomeBatch').onclick = close;
    $('#homeBatchMask').onclick = e => { if (e.target.id === 'homeBatchMask') close(); };

    const groups = [];
    const chunk = 4;
    for (let i = 0; i < picked.length; i += chunk) {
      const part = picked.slice(i, i + chunk);
      const partRes = await Promise.all(part.map(async it => {
        const { ok, data } = await fetchMagnetsCached(it.url);
        return { item: it, magnets: ok && Array.isArray(data) ? data : [], error: ok ? '' : ((data && data.error) || '加载失败') };
      }));
      groups.push(...partRes);
      const loaded = groups.length;
      const body0 = $('#homeBatchBody');
      if (body0) body0.innerHTML = `<div class="loading-box"><span class="spinner"></span> 正在拉取磁力链… ${loaded}/${picked.length}</div>`;
    }

    const body = $('#homeBatchBody');
    if (!body) return;
    body.innerHTML = groups.map((g, gi) => {
      const head = `<div class="home-batch-head"><div class="home-batch-title">${esc(g.item.title)}</div><span class="muted text-xs">${esc(catName(g.item.category))}</span></div>`;
      if (g.error) return `<div class="home-batch-group" data-gi="${gi}">${head}<div class="err-text">${esc(g.error)}</div></div>`;
      if (!g.magnets.length) return `<div class="home-batch-group" data-gi="${gi}">${head}<div class="muted text-sm">该页未提取到磁力链</div></div>`;
      const rows = g.magnets.map((m, mi) => `<label class="mag">
        <input type="checkbox" data-gi="${gi}" data-mi="${mi}">
        <div class="mag-info">
          <div class="mag-desc">${esc(m.desc || m.name)}</div>
          <div class="mag-hash">${esc((m.magnet || '').slice(0, 80))}${(m.magnet || '').length > 80 ? '…' : ''}</div>
        </div>
      </label>`).join('');
      return `<div class="home-batch-group" data-gi="${gi}">${head}${rows}</div>`;
    }).join('');

    const chosen = new Map();
    const syncPushBtn = () => {
      const b = $('#btnHomeBatchPush');
      if (b) { b.disabled = chosen.size === 0; b.textContent = chosen.size ? `推送到 2dland（${chosen.size}）` : '推送到 2dland'; }
    };
    body.querySelectorAll('input[type=checkbox]').forEach(cb => {
      cb.onchange = () => {
        const g = groups[+cb.dataset.gi];
        const m = g && g.magnets[+cb.dataset.mi];
        if (!m) return;
        if (cb.checked) chosen.set(m.magnet, { name: m.name || m.desc || g.item.title, magnet: m.magnet, category: g.item.category, title: g.item.title });
        else chosen.delete(m.magnet);
        cb.closest('.mag').classList.toggle('checked', cb.checked);
        syncPushBtn();
      };
    });
    const allMag = $('#homeBatchAll');
    if (allMag) allMag.onchange = () => {
      body.querySelectorAll('input[type=checkbox]').forEach(cb => {
        if (cb.checked === allMag.checked) return;
        cb.checked = allMag.checked;
        cb.dispatchEvent(new Event('change'));
      });
    };
    $('#btnHomeBatchPush').onclick = async () => {
      const items = Array.from(chosen.values());
      if (!items.length) { toast('请勾选要推送的磁力链', 'error'); return; }
      const btn = $('#btnHomeBatchPush');
      btn.disabled = true; btn.textContent = '推送中…';
      const { ok, data } = await api.post('/api/push', { magnets: items });
      if (!ok) { toast(data.error || '推送失败', 'error'); btn.disabled = false; syncPushBtn(); return; }
      close();
      state.homePicked.clear();
      renderHomeGrid();
      showPushResult(data);
    };
  }
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
          <button class="btn sm ghost" data-sub-result="${i}">⭐ 关注</button>
          <button class="btn sm ghost" data-mag="${i}">查看磁力链 ▾</button>
        </div>
        <div class="result-body" id="mags-${i}"></div>
      </div>`).join('');
    $$('[data-mag]').forEach(b => b.onclick = () => toggleMags(+b.dataset.mag));
    $$('[data-sub-result]').forEach(b => b.onclick = async () => {
      const r = state.lastResults[+b.dataset.subResult];
      addFollowDialog({ keyword: r.title, sixv_url: r.url, category: r.category });
    });
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
        <button class="btn primary" id="btnMoveDone">📦 转移已下载</button>
        <button class="btn" id="btnBatchOrg" disabled>✨ 批量整理 <span id="orgCount">0</span></button>
        <button class="btn danger" id="btnBatchDel" disabled>🗑 批量删除 <span id="selCount">0</span></button>
        <button class="btn" id="btnProbeOrg">🔎 探测已整理</button>
        <button class="btn" id="btnRefreshTasks">⟳ 刷新</button>
      </div>
    </div>
    <div class="card"><div id="tasksList" class="muted text-sm"><span class="spinner"></span> 加载中…</div></div>`;
  }
  function bindTasks() {
    $('#btnRefreshTasks').onclick = () => loadTasks();
    $('#btnMoveDone').onclick = moveDoneTasks;
    $('#btnBatchDel').onclick = batchDelete;
    $('#btnBatchOrg').onclick = batchOrganize;
    $('#btnProbeOrg').onclick = probeOrganized;
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
      const orgCls = t.organized ? ' organized' : '';
      return `<div class="task${orgCls}">
        <input type="checkbox" class="t-check t-item" data-id="${esc(t.identity)}" ${checked}>
        <div class="t-name">
          <div class="t-title">${esc(name)}</div>
          ${t.save_path ? `<div class="muted text-xs" title="${esc(t.save_path)}">📁 ${esc(t.save_path)}</div>` : ''}
          ${t.organized ? `<span class="org-tag">已整理</span>` : ''}
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
    const orgN = selectedCompletedTasks().length;
    const orgBtn = $('#btnBatchOrg'), orgCnt = $('#orgCount');
    if (orgBtn) orgBtn.disabled = orgN === 0;
    if (orgCnt) orgCnt.textContent = orgN;
  }
  function selectedCompletedTasks() {
    return (state.taskAll || []).filter(t => state.taskSel.has(t.identity) && t.status === 1000 && t.save_path);
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
      message: `确定要删除任务「<b>${esc(name)}</b>」吗？<br><span class="muted text-xs">安全保护已启用：只删除任务记录，下载文件一律保留。</span>`,
      checkboxLabel: '',
    });
    if (!r.ok) return;
    const { ok, data } = await api.post('/api/tasks/delete', { identities: [identity], delete_files: false });
    if (!ok) { toast(data.error || '删除失败', 'error'); return; }
    toast('已删除任务，下载文件已保留', 'success');
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
    // 带上任务名：整理记录里要保留「原来的题目」。
    const { ok, data } = await api.post('/api/tasks/organize', { save_path: savePath, name: name || '' });
    if (!ok) { toast(data.error || '整理失败', 'error'); return; }
    const t = (state.taskAll || []).find(tt => tt.save_path === savePath);
    if (t) t.organized = true;
    renderTaskPage();
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
          <div class="text-xs muted mb-8">🧾 已记入「📝 整理记录」：任务名、整理前路径与逐条改名都在那里，可随时回溯原名</div>
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
    const ids = Array.from(state.taskSel).filter(Boolean);
    if (!ids.length) { toast('请先勾选要删除的任务', 'error'); return; }
    const r = await confirmDialog({
      title: '批量删除任务',
      message: `确定要删除选中的 <b>${ids.length}</b> 个任务吗？（跨页选中也会一并删除）<br><span class="muted text-xs">安全保护已启用：只删除任务记录，下载文件一律保留。</span>`,
      checkboxLabel: '',
    });
    if (!r.ok) return;
    const btn = $('#btnBatchDel');
    const old = btn ? btn.innerHTML : '';
    if (btn) { btn.disabled = true; btn.textContent = '删除中…'; }
    const { ok, data } = await api.post('/api/tasks/delete', { identities: ids, delete_files: false });
    if (btn) { btn.disabled = false; btn.innerHTML = old; }
    if (!ok) { toast(data.error || '删除失败', 'error'); return; }
    const n = data.deleted != null ? data.deleted : ids.length;
    const fail = (data.failed || []).length;
    if (fail) toast(`删掉 ${n} 条，失败 ${fail} 条`, 'error');
    else toast(`已删除 ${n} 个任务，下载文件已保留`, 'success');
    loadTasks();
  }
  async function moveDoneTasks() {
    // 这里不能排除 organized=true：历史上已经整理/转移过但任务记录未删除的项目，
    // 也需要通过“转移已下载”清理离线任务记录。按 save_path 去重，避免同一季多集
    // 共享目录时对同一个目录反复 OrganizeTask，引发 404/竞态。
    const completedTasks = (state.taskAll || []).filter(t => t.status === 1000 && t.save_path);
    if (!completedTasks.length) { toast('没有需要转移的已完成任务', 'info'); return; }
    const seen = new Set();
    const tasks = completedTasks.filter(t => {
      if (seen.has(t.save_path)) return false;
      seen.add(t.save_path);
      return true;
    });
    const r = await confirmDialog({
      title: '转移已下载',
      message: `将扫描 <b>${completedTasks.length}</b> 个已完成任务（<b>${tasks.length}</b> 个目录）：有视频的会转移到「已下载」并整理；已转移过的会清理任务记录；空目录/未落盘会自动跳过。`,
      confirmText: '开始转移',
      danger: false,
      checkboxLabel: '',
    });
    if (!r.ok) return;
    const btn = $('#btnMoveDone');
    const old = btn ? btn.innerHTML : '';
    if (btn) { btn.disabled = true; btn.textContent = '转移中…'; }
    const items = tasks.map(t => ({ save_path: t.save_path, name: t.name || t.url || '未命名' }));
    const { ok, data } = await api.post('/api/tasks/organize-batch', { items });
    if (btn) { btn.disabled = false; btn.innerHTML = old; }
    if (!ok) { toast(data.error || '转移失败', 'error'); return; }
    const rows = data.items || [];
    const movedRows = rows.filter(x => x.ok && x.result && x.result.archive_path);
    const okN = movedRows.length;
    const skipN = rows.length - okN;

    // 转移/整理成功后自动删除离线任务，但不删除已经转移到“已下载”的文件。
    const movedPaths = new Set(movedRows.map(x => x.save_path));
    const deleteIds = completedTasks.filter(t => movedPaths.has(t.save_path)).map(t => t.identity).filter(Boolean);
    let delMsg = '';
    if (deleteIds.length) {
      const del = await api.post('/api/tasks/delete', { identities: deleteIds, delete_files: false });
      if (del.ok) delMsg = `，已删除任务 ${deleteIds.length} 个`;
      else delMsg = `，但删除任务失败：${esc((del.data && del.data.error) || '未知错误')}`;
    }

    toast(`转移完成：${okN} 个进入已下载，${skipN} 个跳过/无需处理${delMsg}`, okN ? 'success' : 'info');
    loadTasks();
  }
  async function batchOrganize() {
    const items = selectedCompletedTasks().map(t => ({ save_path: t.save_path, name: t.name || t.url || '未命名' }));
    if (!items.length) { toast('请勾选已完成且有保存目录的任务', 'error'); return; }
    const r = await confirmDialog({
      title: '批量整理',
      message: `整理选中的 <b>${items.length}</b> 个已完成任务？<br><span class="muted text-xs">删广告 + 规范命名。已整理过的会跳过或重扫。</span>`,
      confirmText: '开始整理',
      danger: false,
      checkboxLabel: '',
    });
    if (!r.ok) return;
    const btn = $('#btnBatchOrg');
    const old = btn ? btn.innerHTML : '';
    if (btn) { btn.disabled = true; btn.textContent = '整理中…'; }
    const { ok, data } = await api.post('/api/tasks/organize-batch', { items });
    if (btn) { btn.disabled = false; btn.innerHTML = old; }
    if (!ok) { toast(data.error || '批量整理失败', 'error'); return; }
    const rows = data.items || [];
    const okN = rows.filter(x => x.ok).length;
    toast(`整理完成：${okN}/${rows.length} 成功`, okN === rows.length ? 'success' : 'error');
    rows.forEach(x => {
      if (x.ok) {
        const t = (state.taskAll || []).find(tt => tt.save_path === x.save_path);
        if (t) t.organized = true;
      }
    });
    renderTaskPage();
  }
  async function probeOrganized() {
    const btn = $('#btnProbeOrg');
    if (btn) { btn.disabled = true; btn.textContent = '探测中…'; }
    const { ok, data } = await api.post('/api/tasks/probe', {});
    if (btn) { btn.disabled = false; btn.textContent = '🔎 探测已整理'; }
    if (!ok) { toast(data.error || '探测失败', 'error'); return; }
    state.taskAll = data || [];
    const n = state.taskAll.filter(t => t.organized).length;
    toast(n ? `探测到 ${n} 个已整理` : '没有探测到已整理项目', n ? 'success' : 'info');
    renderTaskPage();
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

  // ============ 视图：整理记录 ============
  // 每次整理（自动/手动/批量）都留一条记录，用于回溯「原来的题目」：
  // 任务名（BT 原始名）、整理前的原始路径、解析出的标题目录，以及逐条重命名映射。
  function triggerInfo(t) {
    if (t === 'auto') return { label: '自动', cls: 'auto' };
    if (t === 'manual') return { label: '手动', cls: 'manual' };
    if (t === 'batch') return { label: '批量', cls: 'batch' };
    return { label: t || '未知', cls: '' };
  }
  function fmtLogTime(s) {
    if (!s) return '';
    const d = new Date(s);
    if (isNaN(d.getTime())) return s;
    const p = n => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
  }
  function viewOrganizeLogs() {
    return `
    <div class="page-head">
      <div><h2>整理记录</h2><div class="desc">每次整理都留痕（任务名 / 原始路径 / 重命名映射）：整理错了也能查回原来的题目是什么</div></div>
      <div class="row">
        <button class="btn sm" id="orgLogRefresh">⟳ 刷新</button>
        <button class="btn sm danger" id="orgLogClear">🗑 清空记录</button>
      </div>
    </div>
    <div class="toolbar">
      <input class="input orglog-filter" id="orgLogFilter" placeholder="筛选：任务名 / 原标题 / 文件名 / 路径">
      <span class="muted text-xs" id="orgLogStat">加载中…</span>
    </div>
    <div class="orglog-list" id="orgLogList"></div>`;
  }
  function bindOrganizeLogs() {
    $('#orgLogRefresh').onclick = loadOrganizeLogs;
    $('#orgLogClear').onclick = async () => {
      const r = await confirmDialog({
        title: '清空整理记录',
        message: '确定清空全部整理记录吗？<br><span class="muted text-xs">只删记录，不会动网盘文件，也不会动下载任务。</span>',
        confirmText: '清空',
        checkboxLabel: '',
      });
      if (!r.ok) return;
      const { ok, data } = await api.post('/api/organize-logs/clear', {});
      if (!ok) { toast(data.error || '清空失败', 'error'); return; }
      state.orgLogs = []; state.orgLogsTotal = 0;
      renderOrganizeLogs();
      toast(`已清空 ${data.cleared || 0} 条整理记录`, 'success');
    };
    const f = $('#orgLogFilter');
    f.value = state.orgLogsFilter || '';
    f.oninput = () => { state.orgLogsFilter = f.value.trim(); renderOrganizeLogs(); };
  }
  async function loadOrganizeLogs() {
    const { ok, data } = await api.get('/api/organize-logs?limit=500');
    if (!ok) { toast(data.error || '加载整理记录失败', 'error'); return; }
    state.orgLogs = data.items || [];
    state.orgLogsTotal = data.total || state.orgLogs.length;
    renderOrganizeLogs();
  }
  // orgLogText 把一条记录拍平成可搜索的文本（含重命名前后的文件名）。
  function orgLogText(r) {
    const parts = [r.task_name, r.save_path, r.archive_path, r.title_dir, r.category, r.error];
    (r.renamed || []).forEach(x => { parts.push(x.old); parts.push(x.new); });
    (r.deleted || []).forEach(x => parts.push(x));
    (r.skipped || []).forEach(x => parts.push(x));
    return parts.filter(Boolean).join(' ').toLowerCase();
  }
  function renderOrganizeLogs() {
    const box = $('#orgLogList');
    if (!box) return;
    const all = state.orgLogs || [];
    const q = (state.orgLogsFilter || '').toLowerCase();
    const rows = q ? all.filter(r => orgLogText(r).includes(q)) : all;
    const stat = $('#orgLogStat');
    if (stat) stat.textContent = all.length
      ? `显示 ${rows.length} / 已加载 ${all.length} 条（服务端共 ${state.orgLogsTotal} 条，最多保留 500 条）`
      : '暂无整理记录';
    if (!rows.length) {
      box.innerHTML = `<div class="card"><div class="muted text-sm">${all.length
        ? '没有匹配的记录，换个关键词试试（可搜任务名、原标题、原文件名、路径）'
        : '还没有整理记录。添加下载任务后自动整理、或在任务页点 ✨ 手动整理，这里就会出现记录。'}</div></div>`;
      return;
    }
    box.innerHTML = rows.map(orgLogCard).join('');
    $$('.orglog-del').forEach(b => b.onclick = () => deleteOrganizeLog(b.dataset.id));
  }
  function orgLogCard(r) {
    const tg = triggerInfo(r.trigger);
    const status = r.ok ? '<span class="tag ok">成功</span>' : '<span class="tag err">失败</span>';
    const rn = r.renamed || [], del = r.deleted || [], sk = r.skipped || [];
    const rnHtml = rn.length ? `
      <div class="orglog-sec">重命名视频（原文件名 → 新文件名）</div>
      <ul class="org-list">${rn.slice(0, 60).map(x => `<li>✏️ <s>${esc(x.old)}</s> → <b>${esc(x.new)}</b></li>`).join('')}${rn.length > 60 ? `<li class="muted">…还有 ${rn.length - 60} 条（完整内容见服务端 organize_logs.json）</li>` : ''}</ul>` : '';
    const delHtml = del.length ? `
      <details class="orglog-more"><summary>🗑 删除广告/杂项（${del.length}）</summary>
        <ul class="org-list">${del.map(n => `<li>🗑 ${esc(n)}</li>`).join('')}</ul>
      </details>` : '';
    const skHtml = sk.length ? `
      <details class="orglog-more"><summary>⏭️ 跳过（${sk.length}）</summary>
        <div class="muted text-xs">剧集文件解析不到集数、或重命名失败时会跳过并保留原名</div>
        <ul class="org-list">${sk.map(n => `<li>⏭️ ${esc(n)}</li>`).join('')}</ul>
      </details>` : '';
    return `
    <div class="card orglog-card">
      <div class="orglog-head">
        <div class="row">
          <span class="tag t-${tg.cls}">${tg.label}</span>
          ${status}
          <b class="orglog-name">${esc(r.task_name || '(任务名未知)')}</b>
        </div>
        <div class="row">
          <span class="muted text-xs">${esc(fmtLogTime(r.time))}</span>
          <button class="icon-btn orglog-del" data-id="${esc(r.id)}" title="删除这条记录">🗑</button>
        </div>
      </div>
      ${r.error ? `<div class="orglog-err">⚠️ ${esc(r.error)}</div>` : ''}
      <div class="orglog-kv"><span class="k">原标题目录</span><span class="v">${esc(r.title_dir || '(未解析到)')}${r.is_tv && r.season ? ` · 第${r.season}季` : ''}${r.category ? ` · ${esc(catName(r.category))}` : ''}</span></div>
      <div class="orglog-kv"><span class="k">整理前路径</span><span class="v">${esc(r.save_path || '')}</span></div>
      ${r.archive_path ? `<div class="orglog-kv"><span class="k">归档后路径</span><span class="v">${esc(r.archive_path)}</span></div>` : ''}
      <div class="orglog-counts">重命名 ${rn.length} · 删除 ${del.length} · 跳过 ${sk.length}</div>
      ${rnHtml}${delHtml}${skHtml}
    </div>`;
  }
  async function deleteOrganizeLog(id) {
    if (!id) return;
    const { ok, data } = await api.post('/api/organize-logs/delete', { ids: [id] });
    if (!ok) { toast(data.error || '删除失败', 'error'); return; }
    state.orgLogs = (state.orgLogs || []).filter(r => r.id !== id);
    state.orgLogsTotal = Math.max(0, (state.orgLogsTotal || 1) - 1);
    renderOrganizeLogs();
    toast('已删除该条记录', 'success');
  }

  // ============ 视图：关注 ============
  function viewFollows() {
    return `
    <div class="page-head">
      <div><h2>关注</h2><div class="desc">点开关注即可看到 6v 上的全部磁力链接，勾选后批量推送到 2dland 离线下载</div></div>
      <div class="row gap-sm">
        <button class="btn ghost" id="btnRefreshAllFollows" title="逐条刷新全部关注的链接名称">⟳ 全部刷新</button>
        <button class="btn primary" id="btnAddFollow">＋ 添加关注</button>
      </div>
    </div>
    <div id="followsList"><div class="card"><span class="spinner"></span> 加载中…</div></div>`;
  }
  function bindFollows() {
    $('#btnAddFollow').onclick = () => addFollowDialog();
    $('#btnRefreshAllFollows').onclick = refreshAllFollowNames;
  }
  // refreshAllFollowNames 逐条刷新全部关注的链接名称，带进度反馈。
  async function refreshAllFollowNames() {
    const btn = $('#btnRefreshAllFollows');
    if (!btn || btn.disabled) return;
    const { ok, data } = await api.get('/api/follows');
    if (!ok) { toast(data.error || '加载关注失败', 'error'); return; }
    const items = data.follows || [];
    if (!items.length) { toast('暂无关注可刷新', 'error'); return; }
    btn.disabled = true;
    let done = 0, failed = 0;
    for (const f of items) {
      btn.textContent = `⟳ 刷新中 ${done + failed + 1}/${items.length}`;
      try {
        const r = await api.post('/api/follows/rename', { id: f.id });
        if (r.ok) done++; else failed++;
      } catch { failed++; }
    }
    btn.disabled = false;
    btn.textContent = '⟳ 全部刷新';
    toast(`全部刷新完成：成功 ${done}${failed ? `，失败 ${failed}` : ''}`, failed ? 'error' : 'success');
    loadFollows();
  }
  // addFollowDialog 添加/预填关注弹窗；prefill: {keyword, sixv_url, category}
  function addFollowDialog(prefill) {
    const p = prefill || {};
    const root = $('#modal-root');
    const cats = [['dlz','国剧'],['rj','日韩剧'],['mj','欧美剧'],['zydy','动漫'],['zy','综艺'],['dy','电影'],['gydy','国语电影'],['jddy','动画电影'],['3D','3D电影']];
    root.innerHTML = `
    <div class="modal-mask" id="followMask">
      <div class="modal wide">
        <div class="modal-head"><h3>添加关注</h3><button class="icon-btn" id="followX">✕</button></div>
        <div class="modal-body">
          <div class="field"><label>6v 详情链接（推荐：点开关注直接显示该页全部磁力）</label><input id="followURL" class="input" value="${esc(p.sixv_url || '')}" placeholder="如：https://www.6v520.com/zydy/2026-09-15/12345.html"></div>
          <div class="field-row">
            <div class="field"><label>名称 / 关键词（链接失效时搜索用）</label><input id="followKeyword" class="input" value="${esc(p.keyword || '')}" placeholder="如：凡人修仙传"></div>
            <div class="field"><label>分类</label><select id="followCategory" class="input">${cats.map(c => `<option value="${c[0]}" ${p.category === c[0] ? 'selected' : ''}>${c[1]}</option>`).join('')}</select></div>
          </div>
        </div>
        <div class="modal-foot"><button class="btn" id="followCancel">取消</button><button class="btn primary" id="followSave">添加关注</button></div>
      </div>
    </div>`;
    const close = () => { root.innerHTML = ''; };
    $('#followX').onclick = close;
    $('#followCancel').onclick = close;
    $('#followMask').onclick = e => { if (e.target.id === 'followMask') close(); };
    $('#followSave').onclick = async () => {
      const keyword = $('#followKeyword').value.trim();
      const sixv_url = $('#followURL').value.trim();
      if (!keyword && !sixv_url) { toast('请填写名称或 6v 链接', 'warn'); return; }
      const body = { keyword: keyword || sixv_url, title: keyword || sixv_url, category: $('#followCategory').value, sixv_url };
      const { ok, data } = await api.post('/api/follows', body);
      if (!ok) { toast(data.error || '添加失败', 'error'); return; }
      close();
      toast('关注已添加', 'success');
      if (state.view === 'follows') loadFollows();
    };
    $('#followKeyword').focus();
  }
  async function loadFollows() {
    const { ok, data } = await api.get('/api/follows');
    if (!ok) { $('#followsList').innerHTML = `<div class="empty">${esc(data.error || '加载失败')}</div>`; return; }
    renderFollows(data.items || []);
  }
  function renderFollows(items) {
    const box = $('#followsList'); if (!box) return;
    if (!items.length) { box.innerHTML = '<div class="empty"><div class="ico">⭐</div>暂无关注。可在发现/搜索页点「⭐」按钮添加，或点右上角「＋ 添加关注」。</div>'; return; }
    box.innerHTML = items.map(f => {
      const name = f.title || '';
      const keyword = f.keyword || '';
      const shortName = name.length > 42 ? name.slice(0, 42) + '…' : name;
      return `
      <div class="card follow-card" data-fid="${esc(f.id)}">
        <div class="row between">
          <div style="min-width:0;">
            <h3 style="overflow:hidden;text-overflow:ellipsis;white-space:nowrap;" title="${esc(name || keyword)}">${esc(shortName || '<span class="muted">（未抓取名称，点「刷新名称」获取）</span>')}</h3>
            <div class="muted text-sm">关键字：${esc(keyword || '—')} · ${esc(catName(f.category))} · ${f.sixv_url ? '固定链接' : '关键词搜索'} · 添加于 ${esc(String(f.created_at || '').slice(0, 10))}</div>
          </div>
          <div class="row gap-sm">
            <button class="btn sm primary" data-follow-open="${esc(f.id)}">🧲 查看磁力</button>
            <button class="btn sm ghost" data-follow-rename="${esc(f.id)}" title="重新抓取 6v 页面标题">⟳ 刷新名称</button>
            <button class="btn sm ghost" data-follow-del="${esc(f.id)}">删除</button>
          </div>
        </div>
      </div>`;
    }).join('');
    $$('[data-follow-del]').forEach(b => b.onclick = async () => {
      if (!confirm('删除这条关注？')) return;
      const { ok, data } = await api.post('/api/follows/delete', { id: b.dataset.followDel });
      if (!ok) { toast(data.error || '删除失败', 'error'); return; }
      toast('已删除', 'success');
      loadFollows();
    });
    $$('[data-follow-rename]').forEach(b => b.onclick = async () => {
      b.disabled = true; b.textContent = '⟳ 抓取中…';
      const { ok, data } = await api.post('/api/follows/rename', { id: b.dataset.followRename });
      if (!ok) { toast(data.error || '刷新失败', 'error'); b.disabled = false; b.textContent = '⟳ 刷新名称'; return; }
      toast('名称已更新：' + (data.title || '').slice(0, 40), 'success');
      loadFollows();
    });
    $$('[data-follow-open]').forEach(b => b.onclick = () => openFollowMagnets(b.dataset.followOpen, items));
  }
  // openFollowMagnets 弹窗查看某条关注的全部磁力链（优先固定 6v 链接，否则关键词搜索第一条）。
  // 弹窗内勾选，底部直接推送；关闭后可立刻点开下一条关注，无需来回翻页。
  async function openFollowMagnets(id, items) {
    const f = items.find(x => x.id === id); if (!f) return;
    const root = $('#modal-root'); if (!root) return;
    const title = f.title || f.keyword || '关注';
    root.innerHTML = `
    <div class="modal-mask" id="fmMask">
      <div class="modal wide">
        <div class="modal-head"><h3>🧲 ${esc(title)} <span class="muted text-sm">· ${esc(catName(f.category))}</span></h3><button class="icon-btn" id="fmX">✕</button></div>
        <div class="modal-body" id="fmBody"><div class="muted text-sm"><span class="spinner"></span> 正在抓取全部磁力链…</div></div>
        <div class="modal-foot">
          <label class="auto-refresh" style="margin-right:auto;"><input type="checkbox" id="fmAll"> 全选</label>
          <button class="btn" id="fmCancel">关闭</button>
          <button class="btn primary" id="fmPush" disabled>推送到 2dland</button>
        </div>
      </div>
    </div>`;
    const body = $('#fmBody');
    const close = () => { root.innerHTML = ''; };
    $('#fmX').onclick = close;
    $('#fmCancel').onclick = close;
    $('#fmMask').onclick = e => { if (e.target.id === 'fmMask') close(); };

    let url = f.sixv_url;
    if (!url) {
      const s = await api.get('/api/search?q=' + encodeURIComponent(f.keyword || f.title || ''));
      if (s.ok && Array.isArray(s.data) && s.data.length) url = s.data[0].url;
    }
    if (!url) { body.innerHTML = '<div class="err-text">未找到资源链接，请删除后重新添加并填写 6v 链接</div>'; return; }
    const res = await api.get('/api/magnets?url=' + encodeURIComponent(url));
    if (!res.ok || !Array.isArray(res.data) || !res.data.length) {
      body.innerHTML = `<div class="err-text">${esc((res.data && res.data.error) || '该页未提取到磁力链')}</div>`;
      return;
    }
    const magnets = res.data;
    body.innerHTML = `<div class="muted text-sm mb-8">共 ${magnets.length} 条磁力</div>` + magnets.map((m, idx) => `<label class="mag">
        <input type="checkbox" data-fmidx="${idx}">
        <div class="mag-info">
          <div class="mag-desc">${esc(m.desc || m.name)}</div>
          <div class="mag-hash">${esc((m.magnet || '').slice(0, 80))}…</div>
        </div>
      </label>`).join('');

    const chosen = new Map();
    const syncPush = () => {
      const b = $('#fmPush');
      if (b) { b.disabled = chosen.size === 0; b.textContent = chosen.size ? `推送到 2dland（${chosen.size}）` : '推送到 2dland'; }
    };
    body.querySelectorAll('input[type=checkbox][data-fmidx]').forEach(cb => {
      cb.onchange = () => {
        const m = magnets[+cb.dataset.fmidx];
        if (!m) return;
        if (cb.checked) chosen.set(m.magnet, { name: m.name || m.desc || f.title, magnet: m.magnet, category: f.category, title: f.title });
        else chosen.delete(m.magnet);
        cb.closest('.mag').classList.toggle('checked', cb.checked);
        syncPush();
      };
    });
    const all = $('#fmAll');
    if (all) all.onchange = () => {
      body.querySelectorAll('input[type=checkbox][data-fmidx]').forEach(cb => {
        if (cb.checked !== all.checked) { cb.checked = all.checked; cb.dispatchEvent(new Event('change')); }
      });
    };
    $('#fmPush').onclick = async () => {
      const list = Array.from(chosen.values());
      if (!list.length) { toast('请勾选要推送的磁力链', 'error'); return; }
      const btn = $('#fmPush');
      btn.disabled = true; btn.textContent = '推送中…';
      const { ok, data } = await api.post('/api/push', { magnets: list });
      if (!ok) { toast(data.error || '推送失败', 'error'); btn.disabled = false; syncPush(); return; }
      showPushResult(data);
      chosen.clear();
    };
  }

  // ============ 视图：文件管理 ============
  function viewFiles() {
    return `
    <div class="page-head">
      <div><h2>文件管理</h2><div class="desc">直接管理 2dland 网盘文件，可拖拽文件到文件夹移动</div></div>
      <div class="row gap-sm">
        <button class="btn" id="btnNewFolder">📁 新建文件夹</button>
        <button class="btn" id="btnFileOrganize" disabled>✨ 批量整理 <span id="fileOrgCount"></span></button>
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
    $('#btnFileOrganize').onclick = organizeSelectedFolders;
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
      const date = formatFileDate(f.update_ts);
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
  function selectedFileDirs() {
    return (state.fileItems || []).filter(f => f.dir && state.fileSel.has(f.identity));
  }
  function updateFileSelUI() {
    const n = state.fileSel.size;
    const btn = $('#btnFileDel'), cnt = $('#fileSelCount');
    if (btn) btn.disabled = n === 0;
    if (cnt) cnt.textContent = n > 0 ? `(${n})` : '';
    const dirs = selectedFileDirs().length;
    const orgBtn = $('#btnFileOrganize'), orgCnt = $('#fileOrgCount');
    if (orgBtn) orgBtn.disabled = dirs === 0;
    if (orgCnt) orgCnt.textContent = dirs > 0 ? `(${dirs})` : '';
  }
  async function organizeSelectedFolders() {
    const dirs = selectedFileDirs();
    if (!dirs.length) { toast('请先勾选要整理的文件夹', 'error'); return; }
    const r = await confirmDialog({
      title: '批量整理文件夹',
      message: `整理选中的 <b>${dirs.length}</b> 个文件夹？<br><span class="muted text-xs">会按文件夹路径执行：转移到已下载（如尚未在已下载中）、删除广告/杂项、移动视频到项目根、规范化文件名。</span>`,
      confirmText: '开始整理',
      danger: false,
      checkboxLabel: '',
    });
    if (!r.ok) return;
    const btn = $('#btnFileOrganize');
    const old = btn ? btn.innerHTML : '';
    if (btn) { btn.disabled = true; btn.textContent = '整理中…'; }
    const items = dirs.map(f => ({ save_path: f.path, name: f.name }));
    const { ok, data } = await api.post('/api/tasks/organize-batch', { items });
    if (btn) { btn.disabled = false; btn.innerHTML = old; }
    if (!ok) { toast(data.error || '批量整理失败', 'error'); return; }
    const rows = data.items || [];
    const okN = rows.filter(x => x.ok).length;
    toast(`整理完成：${okN}/${rows.length} 成功`, okN === rows.length ? 'success' : 'error');
    loadFiles();
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
      message: `确定要删除选中的 <b>${ids.length}</b> 项吗？（移到回收站，可在 2dland 恢复）<br><span class="muted text-xs">安全保护：已下载媒体库中的内容无法在本应用删除。</span>`,
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
  function formatFileDate(ts) {
    const n = Number(ts);
    if (!Number.isFinite(n) || n <= 0) return '';
    const ms = n < 100000000000 ? n * 1000 : n;
    const d = new Date(ms);
    return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString('zh-CN');
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
          <div class="row">
            <button class="btn sm" id="btnClearRateLimit">清除推送限流</button>
            <button class="btn sm danger" id="btn2dlandLogout">退出 2dland 登录</button>
          </div>
        </div>
      </div>`;
      $('#btnClearRateLimit').onclick = async () => {
        const {ok,data} = await api.post('/api/drive/clear-rate-limit',{});
        toast(ok?'限流状态已清除':(data.error||'清除失败'), ok?'success':'error');
      };
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
