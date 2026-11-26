/*
 * bnyc-remote address book overlay for CrossDesk Web Client.
 *
 * 注入方式（在 crossdesk-web-client 的 index.html 末尾加入）：
 *   <link rel="stylesheet" href="addressbook.css">
 *   <script>
 *     window.BNYC_ADDRESSBOOK = { portalUrl: "https://addr.example.com" };
 *   </script>
 *   <script src="addressbook.js" defer></script>
 *
 * 设计：
 * - 不修改官方 web_client.js，只操作 DOM：#transmission-id / #transmission-pwd / #connection-form
 * - 登录态存 sessionStorage，关闭标签页即失效
 * - 点击设备后自动填表并提交，复用官方 connect() 逻辑
 * - 未配置 portalUrl 时完全不激活，不影响原站
 */
(function () {
  'use strict';

  var cfg = window.BNYC_ADDRESSBOOK || {};
  var portalUrl = String(cfg.portalUrl || '').replace(/\/+$/, '');
  if (!portalUrl) return;

  var TOKEN_KEY = 'bnyc_portal_token';
  var state = { token: sessionStorage.getItem(TOKEN_KEY) || '', devices: [], loginError: '', busy: false };

  var root, panel, listEl, loginView, loginError, deviceView, toastEl;
  var open = false;

  function el(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text != null) n.textContent = text;
    return n;
  }

  function api(path, opts) {
    opts = opts || {};
    var headers = { 'Content-Type': 'application/json' };
    if (state.token) headers.Authorization = 'Bearer ' + state.token;
    return fetch(portalUrl + path, {
      method: opts.method || 'GET',
      headers: headers,
      body: opts.body ? JSON.stringify(opts.body) : undefined
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (data) {
        if (!res.ok) throw new Error(data.error || ('HTTP ' + res.status));
        return data;
      });
    });
  }

  function toast(msg, isError) {
    if (!toastEl) {
      toastEl = el('div', 'bnyc-toast');
      document.body.appendChild(toastEl);
    }
    toastEl.textContent = msg;
    toastEl.className = 'bnyc-toast bnyc-show' + (isError ? ' bnyc-error' : '');
    clearTimeout(toastEl._t);
    toastEl._t = setTimeout(function () { toastEl.className = 'bnyc-toast'; }, 2600);
  }

  function setPanelOpen(next) {
    open = next;
    panel.classList.toggle('bnyc-open', open);
  }

  function render() {
    listEl.innerHTML = '';
    if (!state.token) {
      loginView.style.display = 'block';
      deviceView.style.display = 'none';
      loginError.textContent = state.loginError || '';
      return;
    }
    loginView.style.display = 'none';
    deviceView.style.display = 'block';

    if (!state.devices.length) {
      listEl.appendChild(el('div', 'bnyc-empty', '地址簿为空，请先在门户里添加设备。'));
      return;
    }
    state.devices.forEach(function (d) {
      var item = el('div', 'bnyc-device');
      var info = el('div', 'bnyc-device-info');
      info.appendChild(el('div', 'bnyc-device-name', d.name || '未命名'));
      var meta = el('div', 'bnyc-device-meta', d.remote_id + (d.note ? ' · ' + d.note : ''));
      info.appendChild(meta);
      var btn = el('button', 'bnyc-connect', '连接');
      btn.onclick = function () { connect(d, btn); };
      item.appendChild(info);
      item.appendChild(btn);
      listEl.appendChild(item);
    });
  }

  function login(username, password) {
    if (state.busy) return;
    state.busy = true;
    state.loginError = '';
    api('/api/login', { method: 'POST', body: { username: username, password: password } })
      .then(function (data) {
        state.token = data.token;
        sessionStorage.setItem(TOKEN_KEY, state.token);
        return loadDevices();
      })
      .catch(function (e) {
        state.token = '';
        sessionStorage.removeItem(TOKEN_KEY);
        state.loginError = '登录失败：' + e.message;
        render();
      })
      .finally(function () { state.busy = false; });
  }

  function loadDevices() {
    return api('/api/devices').then(function (list) {
      state.devices = Array.isArray(list) ? list : [];
      render();
    });
  }

  function connect(device, btn) {
    var idInput = document.getElementById('transmission-id');
    var pwdInput = document.getElementById('transmission-pwd');
    var form = document.getElementById('connection-form');
    if (!idInput || !pwdInput || !form) {
      toast('未找到 CrossDesk 连接表单', true);
      return;
    }
    if (!device.password) {
      toast('该设备密码未能解密，请检查门户配置', true);
      return;
    }
    btn.disabled = true;
    btn.textContent = '连接中';
    idInput.value = device.remote_id;
    pwdInput.value = device.password;
    idInput.dispatchEvent(new Event('input', { bubbles: true }));
    pwdInput.dispatchEvent(new Event('input', { bubbles: true }));
    setPanelOpen(false);
    try {
      if (typeof form.requestSubmit === 'function') form.requestSubmit();
      else form.dispatchEvent(new Event('submit', { bubbles: true, cancelable: true }));
    } catch (e) {
      toast('发起连接失败：' + e.message, true);
    }
    setTimeout(function () { btn.disabled = false; btn.textContent = '连接'; }, 2000);
  }

  function logout() {
    api('/api/logout', { method: 'POST' }).catch(function () {});
    state.token = '';
    state.devices = [];
    sessionStorage.removeItem(TOKEN_KEY);
    render();
  }

  function build() {
    root = el('div', 'bnyc-ab');
    var fab = el('button', 'bnyc-fab', '地址簿');
    fab.title = '打开地址簿';
    fab.onclick = function () { setPanelOpen(!open); };

    panel = el('div', 'bnyc-panel');
    var head = el('div', 'bnyc-head');
    head.appendChild(el('span', 'bnyc-title', '我的地址簿'));
    var close = el('button', 'bnyc-close', '×');
    close.onclick = function () { setPanelOpen(false); };
    head.appendChild(close);

    loginView = el('div', 'bnyc-login');
    var u = el('input', 'bnyc-input');
    u.placeholder = '用户名';
    u.autocomplete = 'username';
    var p = el('input', 'bnyc-input');
    p.type = 'password';
    p.placeholder = '密码';
    p.autocomplete = 'current-password';
    loginError = el('div', 'bnyc-login-error');
    var submit = el('button', 'bnyc-primary', '登录');
    submit.onclick = function () { login(u.value.trim(), p.value); };
    p.onkeydown = function (e) { if (e.key === 'Enter') submit.click(); };
    loginView.appendChild(u);
    loginView.appendChild(p);
    loginView.appendChild(loginError);
    loginView.appendChild(submit);

    deviceView = el('div', 'bnyc-devices');
    listEl = el('div', 'bnyc-list');
    var refresh = el('button', 'bnyc-link', '刷新');
    refresh.onclick = function () { loadDevices().catch(function (e) { toast(e.message, true); }); };
    var out = el('button', 'bnyc-link', '退出登录');
    out.onclick = logout;
    var foot = el('div', 'bnyc-foot');
    foot.appendChild(refresh);
    foot.appendChild(out);
    deviceView.appendChild(listEl);
    deviceView.appendChild(foot);

    panel.appendChild(head);
    panel.appendChild(loginView);
    panel.appendChild(deviceView);
    root.appendChild(panel);
    root.appendChild(fab);
    document.body.appendChild(root);

    render();
    if (state.token) {
      loadDevices().catch(function () { logout(); });
    }
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', build);
  } else {
    build();
  }
})();
