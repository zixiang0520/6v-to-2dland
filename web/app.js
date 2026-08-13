'use strict';
(function () {
  const $ = selector => document.querySelector(selector);
  const esc = value => String(value ?? '').replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[char]));
  const defaultURL = 'https://www.kdocs.cn/l/cmMxolBHuN1p';
  const state = { session: null, view: 'resources', query: '', data: null, settings: null };

  async function request(path, options = {}) {
    let response;
    try {
      response = await fetch(path, { headers: { 'Content-Type': 'application/json' }, ...options });
    } catch (error) {
      return { ok: false, data: { error: '网络错误：' + error.message } };
    }
    const text = await response.text();
    let data;
    try { data = text ? JSON.parse(text) : {}; } catch (_) { data = { error: text || '响应解析失败' }; }
    if (response.status === 401 && state.session) {
      state.session.logged_in = false;
      render();
    }
    return { ok: response.ok, data };
  }
  const get = path => request(path);
  const post = (path, body) => request(path, { method: 'POST', body: JSON.stringify(body || {}) });

  function toast(message, type = 'info') {
    const item = document.createElement('div');
    item.className = 'toast ' + type;
    item.textContent = message;
    $('#toast').appendChild(item);
    setTimeout(() => item.remove(), 3200);
  }

  async function copy(text) {
    try {
      await navigator.clipboard.writeText(text);
    } catch (_) {
      const area = document.createElement('textarea');
      area.value = text;
      document.body.appendChild(area);
      area.select();
      document.execCommand('copy');
      area.remove();
    }
    toast('已复制', 'success');
  }

  function setupView() {
    return `<main class="auth-screen"><section class="auth-card">
      <div class="mark">KD</div><h1>初始化资源搜索</h1>
      <p class="muted">设置 Web 访问密码和公开金山文档地址。</p>
      <form id="setupForm">
        <label>访问密码<input class="input" id="setupPassword" type="password" autocomplete="new-password" required></label>
        <label>金山文档 URL<input class="input" id="setupURL" type="url" value="${defaultURL}" required></label>
        <button class="btn primary block" type="submit">完成初始化</button>
      </form>
    </section></main>`;
  }

  function loginView() {
    return `<main class="auth-screen"><section class="auth-card">
      <div class="mark">KD</div><h1>访问登录</h1><p class="muted">请输入 Web 访问密码。</p>
      <form id="loginForm">
        <label>访问密码<input class="input" id="loginPassword" type="password" autocomplete="current-password" autofocus required></label>
        <button class="btn primary block" type="submit">登录</button>
      </form>
    </section></main>`;
  }

  function shellView() {
    return `<div class="shell">
      <header><a class="brand" href="#resources"><span class="brand-mark">KD</span>金山文档资源搜索</a>
        <nav><a href="#resources" class="${state.view === 'resources' ? 'active' : ''}">资源</a><a href="#settings" class="${state.view === 'settings' ? 'active' : ''}">设置</a></nav>
        <button class="btn small" id="logout">退出</button>
      </header><main class="content" id="content"></main>
    </div>`;
  }

  function resourcesView() {
    const data = state.data;
    const status = data ? `<div class="status">
      <span>${data.refreshed_at ? '更新于 ' + new Date(data.refreshed_at).toLocaleString('zh-CN') : '尚未刷新'}</span>
      ${data.source_mode ? `<span>抓取方式：${data.source_mode === 'http' ? '直接请求' : '浏览器渲染'}</span>` : ''}
      <span>匹配 ${data.total || 0} 条</span></div>` : '';
    return `<section class="page-head"><div><h1>百度网盘资源</h1><p>按资源标题搜索公开金山文档中的链接。</p></div><button class="btn" id="refresh">刷新文档</button></section>
      <div class="search"><input class="input" id="query" value="${esc(state.query)}" placeholder="输入资源标题" autocomplete="off"><button class="btn primary" id="search">搜索</button></div>
      ${status}<div id="message"></div><div id="results">${renderResults(data && data.resources)}</div>`;
  }

  function renderResults(resources) {
    if (!state.data) return '<div class="empty">点击“刷新文档”抓取资源，或搜索已有缓存。</div>';
    if (!resources || !resources.length) return '<div class="empty">没有匹配的资源。</div>';
    return resources.map((item, index) => `<article class="resource">
      <div class="resource-main"><h2>${esc(item.title)}</h2><a href="${esc(item.url)}" target="_blank" rel="noopener noreferrer">${esc(item.url)}</a>
      <div class="pwd">提取码：<strong>${esc(item.pwd || '无')}</strong></div></div>
      <div class="resource-actions"><button class="btn small" data-copy="${index}">复制</button><a class="btn small primary" href="${esc(item.url)}" target="_blank" rel="noopener noreferrer">打开</a></div>
    </article>`).join('');
  }

  function settingsView() {
    if (!state.settings) return '<div class="card">正在加载设置…</div>';
    return `<section class="page-head"><div><h1>设置</h1><p>更改数据源或 Web 访问密码。</p></div></section>
      <form class="card" id="settingsForm">
        <label>金山文档 URL<input class="input" id="kdocsURL" type="url" value="${esc(state.settings.kdocs_url)}" required></label>
        <label>新访问密码<input class="input" id="newPassword" type="password" placeholder="留空则不修改" autocomplete="new-password"></label>
        <p class="warning">${esc(state.settings.warning)}</p>
        <button class="btn primary" type="submit">保存设置</button>
      </form>`;
  }

  function render() {
    const app = $('#app');
    if (!state.session) { app.innerHTML = '<div class="boot"><div class="spinner"></div><p>正在加载…</p></div>'; return; }
    if (!state.session.auth_required) { app.innerHTML = setupView(); bindSetup(); return; }
    if (!state.session.logged_in) { app.innerHTML = loginView(); bindLogin(); return; }
    app.innerHTML = shellView();
    $('#logout').onclick = logout;
    renderContent();
  }

  function renderContent() {
    $('#content').innerHTML = state.view === 'settings' ? settingsView() : resourcesView();
    if (state.view === 'settings') bindSettings(); else bindResources();
  }

  function bindSetup() {
    $('#setupForm').onsubmit = async event => {
      event.preventDefault();
      const result = await post('/api/ui/setup', { password: $('#setupPassword').value, kdocs_url: $('#setupURL').value.trim() });
      if (!result.ok) return toast(result.data.error || '初始化失败', 'error');
      state.session = { auth_required: true, logged_in: true };
      render();
    };
  }

  function bindLogin() {
    $('#loginForm').onsubmit = async event => {
      event.preventDefault();
      const result = await post('/api/ui/login', { password: $('#loginPassword').value });
      if (!result.ok) return toast(result.data.error || '登录失败', 'error');
      state.session.logged_in = true;
      render();
    };
  }

  function bindResources() {
    $('#search').onclick = search;
    $('#query').onkeydown = event => { if (event.key === 'Enter') search(); };
    $('#refresh').onclick = refresh;
    document.querySelectorAll('[data-copy]').forEach(button => {
      button.onclick = () => {
        const item = state.data.resources[Number(button.dataset.copy)];
        copy(item.title + '\n' + item.url + (item.pwd ? '\n提取码：' + item.pwd : ''));
      };
    });
  }

  async function search() {
    state.query = $('#query').value.trim();
    const result = await get('/api/resources?q=' + encodeURIComponent(state.query));
    if (!result.ok) return toast(result.data.error || '搜索失败', 'error');
    state.data = result.data;
    renderContent();
  }

  async function refresh() {
    const button = $('#refresh');
    button.disabled = true;
    button.textContent = '抓取中…';
    const result = await post('/api/resources/refresh');
    if (!result.ok) {
      button.disabled = false;
      button.textContent = '刷新文档';
      return toast(result.data.error || '刷新失败', 'error');
    }
    state.data = result.data;
    state.query = '';
    toast('资源已刷新', 'success');
    renderContent();
  }

  async function loadSettings() {
    const result = await get('/api/settings');
    if (!result.ok) return toast(result.data.error || '设置加载失败', 'error');
    state.settings = result.data;
    renderContent();
  }

  function bindSettings() {
    const form = $('#settingsForm');
    if (!form) return;
    form.onsubmit = async event => {
      event.preventDefault();
      const newPassword = $('#newPassword').value;
      const result = await post('/api/settings', { kdocs_url: $('#kdocsURL').value.trim(), access_password: newPassword });
      if (!result.ok) return toast(result.data.error || '保存失败', 'error');
      if (result.data.url_changed) state.data = null;
      toast('设置已保存', 'success');
      await loadSettings();
    };
  }

  async function logout() {
    await post('/api/ui/logout');
    state.session.logged_in = false;
    state.data = null;
    render();
  }

  async function route() {
    state.view = location.hash === '#settings' ? 'settings' : 'resources';
    render();
    if (state.session && state.session.logged_in && state.view === 'settings') await loadSettings();
    if (state.session && state.session.logged_in && state.view === 'resources' && !state.data) await search();
  }

  async function init() {
    const result = await get('/api/ui/session');
    state.session = result.ok ? result.data : { auth_required: true, logged_in: false };
    window.addEventListener('hashchange', route);
    await route();
  }

  init();
})();
