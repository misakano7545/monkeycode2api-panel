'use strict';
/* ── 状态 ─────────────────────────────────────────────────────────── */
const LS_KEY = 'mc2a.key', LS_THEME = 'mc2a.theme';
let theme = localStorage.getItem(LS_THEME) || 'auto';   // auto | light | dark
let view = 'accounts';
let overviewData = null, cfgLoaded = null;
let logCh = '', logPin = true, logTimer = null;
let refTimer = null;

const $ = id => document.getElementById(id);

/* ── 主题 ─────────────────────────────────────────────────────────── */
/* 两态翻转（浅/深），首次访问跟随系统偏好；点击总是切换可见外观，符合直觉。 */
function effTheme() {
  return theme === 'auto' ? (matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark') : theme;
}
function applyTheme() {
  const eff = effTheme();
  document.documentElement.dataset.theme = eff;
  $('icoTheme').innerHTML = eff === 'light'
    ? '<circle cx="8" cy="8" r="3"/><path d="M8 1v2M8 13v2M1 8h2M13 8h2M3.2 3.2l1.4 1.4M11.4 11.4l1.4 1.4M12.8 3.2l-1.4 1.4M4.6 11.4l-1.4 1.4"/>'
    : '<path d="M13.2 9.6A5.6 5.6 0 0 1 6.4 2.8a5.6 5.6 0 1 0 6.8 6.8z"/>';
  $('btnTheme').title = eff === 'light' ? '切换到深色' : '切换到浅色';
}
addEventListener('change', applyTheme);
$('btnTheme').onclick = () => {
  theme = effTheme() === 'light' ? 'dark' : 'light';
  localStorage.setItem(LS_THEME, theme);
  applyTheme();
};
applyTheme();

/* ── 手机抽屉导航（≤760px；桌面按钮隐藏、类名无副作用） ─────────────── */
const navEl = document.querySelector('.nav'), navScrim = $('navScrim'), btnNav = $('btnNav');
function navSet(open) {
  if (!navEl || !navScrim) return;
  navEl.classList.toggle('open', open);
  navScrim.classList.toggle('on', open);
  document.body.classList.toggle('nav-open', open);
  if (btnNav) btnNav.setAttribute('aria-expanded', String(open));
}
if (btnNav && navScrim) {
  btnNav.onclick = () => navSet(!navEl.classList.contains('open'));
  navScrim.onclick = () => navSet(false);
}
addEventListener('keydown', e => { if (e.key === 'Escape') navSet(false); });

/* ── 请求 ─────────────────────────────────────────────────────────── */
async function api(path, opts = {}) {
  const h = Object.assign({}, opts.headers || {});
  const k = localStorage.getItem(LS_KEY);
  if (k) h['Authorization'] = 'Bearer ' + k;
  if (opts.body) h['Content-Type'] = 'application/json';
  const r = await fetch('/panel/api/' + path, Object.assign({}, opts, { headers: h }));
  // status 挂到错误上：调用方靠它区分「可重试的抖动」与「重试无意义的终态」。
  if (r.status === 401) { openKey(); const e = new Error('密钥无效或未填写'); e.status = 401; throw e; }
  const d = await r.json().catch(() => ({}));
  if (!r.ok) { const e = new Error(d.error || ('HTTP ' + r.status)); e.status = r.status; throw e; }
  return d;
}
function toast(msg, cls) {
  const el = document.createElement('div');
  el.className = 'tst ' + (cls || '');
  el.textContent = msg;
  $('toasts').appendChild(el);
  setTimeout(() => el.remove(), 3600);
}
// esc 文本/属性双安全转义。不能只用 div.innerHTML（它转义 <>& 但不转义引号），
// 否则字符串拼进 HTML 属性时引号可闭合属性并注入事件处理器。
function esc(s) {
  return String(s == null ? '' : s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}
function ago(iso) {
  if (!iso || iso.startsWith('0001-')) return '—';
  const s = (Date.now() - new Date(iso)) / 1000;
  if (s < 0) return '刚刚';
  if (s < 60) return Math.floor(s) + ' 秒前';
  if (s < 3600) return Math.floor(s / 60) + ' 分钟前';
  if (s < 86400) return Math.floor(s / 3600) + ' 小时前';
  return Math.floor(s / 86400) + ' 天前';
}
function dur(sec) {
  sec = Math.max(0, Math.round(sec));
  const h = Math.floor(sec / 3600), m = Math.floor(sec % 3600 / 60), s = sec % 60;
  return h ? h + '时' + String(m).padStart(2, '0') + '分' : m ? m + '分' + String(s).padStart(2, '0') + '秒' : s + '秒';
}
const num = n => (n == null ? '—' : Number(n).toLocaleString('zh-CN'));
const pct = (a, b) => (b > 0 ? Math.min(100, Math.max(0, a / b * 100)) : 0);

/* ── 密钥门 ───────────────────────────────────────────────────────── */
function openKey() { $('keyVeil').classList.add('on'); setTimeout(() => $('keyInput').focus(), 60); }
$('btnKey').onclick = async () => {
  const v = $('keyInput').value.trim();
  if (!v) return;
  localStorage.setItem(LS_KEY, v);
  try {
    await api('overview');
    $('keyErr').hidden = true;
    $('keyVeil').classList.remove('on');
    start();
  } catch (e) { $('keyErr').hidden = false; }
};
$('keyInput').addEventListener('keydown', e => { if (e.key === 'Enter') $('btnKey').click(); });

// 登出：api_key 是唯一凭据（服务端只做 VerifyBearer 比对，无会话无 cookie 可失效），
// 所以「登出」= 清掉本机保存的密钥并重载——重载把内存里的定时器与已渲染数据一并清空，
// 无密钥时 start() 的首次请求 401 会自动弹回密钥门（data-lock="1"，不可绕过）。
$('btnLogout').onclick = () => {
  if (!confirm('登出将清空本机保存的 API 密钥，需要重新输入才能进入面板。确认登出？')) return;
  localStorage.removeItem(LS_KEY);
  location.reload();
};

/* ── 路由 ─────────────────────────────────────────────────────────── */
const TITLES = { accounts: '账号池', usage: '用量', models: '模型与档位', config: '配置', logs: '运行日志' };
function go(v) {
  navSet(false);
  view = v;
  document.querySelectorAll('.view').forEach(s => s.hidden = s.id !== 'view-' + v);
  document.querySelectorAll('.nav a').forEach(a => a.classList.toggle('on', a.dataset.view === v));
  $('ttl').textContent = TITLES[v];
  if (v === 'models') loadModels();
  if (v === 'config') loadConfig();
  if (v === 'logs') loadLogs();
  if (v === 'usage') loadUsage();
}
document.querySelectorAll('.nav a').forEach(a => a.onclick = e => { e.preventDefault(); go(a.dataset.view); history.replaceState(null, '', '#' + a.dataset.view); });

/* ── 账号池 ───────────────────────────────────────────────────────── */
function accState(a) {
  if (a.disabled) return { cls: 'off', tag: '<span class="tag bad">已停用</span>' };
  if (a.busy) return { cls: '', tag: '<span class="tag ok">跑任务中</span>' };
  if (a.cooldown) return { cls: 'cool', tag: '<span class="tag warn">冷却 ' + esc(a.cool_kind || '') + ' ' + esc(a.cooldown) + '</span>' };
  return { cls: '', tag: '<span class="tag ok">可用</span>' };
}
function accRow(a) {
  const st = accState(a);
  const extra = [];
  if (!a.image_id) extra.push('<span class="tag warn">缺 image_id</span>');
  if (a.expiring) extra.push('<span class="tag warn">临期</span>');
  if (a.wallet_error) extra.push('<span class="tag mute" title="' + esc(a.wallet_error) + '">额度未知</span>');
  const lim = a.daily_token_limit || 0, bal = a.daily_token_balance || 0;
  const quota = lim > 0
    ? '<div class="cred"><div class="n">' + num(bal) + ' <span class="of">/ ' + num(lim) + '</span></div><div class="bar"><i style="width:' + pct(bal, lim).toFixed(1) + '%"></i></div></div>'
    : '<span class="tag mute">未拉取</span>';
  const acts = [];
  acts.push('<button data-act="balance" data-id="' + esc(a.id) + '">额度</button>');
  acts.push('<button data-act="checkin" data-id="' + esc(a.id) + '">签到</button>');
  if (a.cooldown) acts.push('<button data-act="cooldown_clear" data-id="' + esc(a.id) + '">解冷却</button>');
  acts.push('<button data-act="' + (a.disabled ? 'revive' : 'disable') + '" data-id="' + esc(a.id) + '">' + (a.disabled ? '启用' : '停用') + '</button>');
  acts.push('<button class="danger" data-act="remove" data-id="' + esc(a.id) + '">删除</button>');
  return '<tr class="' + st.cls + '">'
    + '<td class="mark" aria-hidden="true"><i></i></td>'
    + '<td class="who"><div class="nm">' + esc(a.name || a.email || a.id) + (a.region === 'intl' ? '<span class="realm-tag">国际</span>' : '') + '</div>'
    + '<div class="id">' + esc(a.id) + (a.user_id ? ' · ' + esc(a.user_id) : '') + '</div></td>'
    + '<td>' + (a.region === 'intl' ? '国际版' : '国内版') + '</td>'
    + '<td>' + st.tag + (extra.length ? ' ' + extra.join(' ') : '') + '</td>'
    + '<td>' + quota + '</td>'
    + '<td class="num">' + num(a.balance) + '</td>'
    + '<td class="num">' + esc(a.last_checkin || '—') + '</td>'
    + '<td class="c-acts"><div class="acts">' + acts.join('') + '</div></td>'
    + '</tr>';
}
async function loadAccounts() {
  const d = await api('overview');
  overviewData = d;
  const list = d.accounts || [];
  $('sTotal').textContent = d.total;
  $('sHealthy').textContent = d.healthy;
  $('sCooling').textContent = d.cooling;
  $('sDisabled').textContent = d.disabled;
  $('sTokens').textContent = num(d.tokens_remaining);
  $('sCredits').textContent = num(d.credits);
  $('navVer').textContent = d.version || 'v-';
  $('navUp').textContent = dur(d.uptime_sec || 0);
  $('navState').textContent = d.auth_required ? '已鉴权' : '未启用鉴权';
  $('navPulse').className = 'pulse' + (d.auth_required ? '' : ' warn');
  $('subMeta').textContent = list.length + ' 个账号 · ' + (d.cooling + d.disabled) + ' 个不可用';
  $('accBody').innerHTML = list.length
    ? list.map(accRow).join('')
    : '<tr><td colspan="8"><div class="empty"><div class="big">还没有账号</div>点右上角 + 粘贴 session 添加</div></td></tr>';
  $('accNote').textContent = d.tokens_remaining ? '今日剩余 ' + num(d.tokens_remaining) + ' token' : '';
}
/* 账号操作统一走事件委托（CSP 禁内联 onclick） */
$('accBody').addEventListener('click', async e => {
  const b = e.target.closest('button[data-act]');
  if (!b) return;
  const id = b.dataset.id, act = b.dataset.act;
  if (act === 'remove' && !confirm('删除账号 ' + id + '？其 auths 文件会一并删除。')) return;
  b.disabled = true;
  try {
    const r = await api('accounts/' + encodeURIComponent(id) + '/' + act, { method: 'POST' });
    toast(r.message || '完成', 'ok');
  } catch (err) { toast(err.message, 'err'); }
  b.disabled = false;
  loadAccounts().catch(() => {});
});

$('btnCheckinAll').onclick = async e => {
  const b = e.currentTarget;
  b.disabled = true;
  try {
    const r = await api('checkin_all', { method: 'POST' });
    toast('签到完成：成功 ' + r.ok + ' / 失败 ' + r.failed, r.failed ? 'err' : 'ok');
  } catch (err) { toast(err.message, 'err'); }
  b.disabled = false;
  loadAccounts().catch(() => {});
};
$('btnBalanceAll').onclick = async e => {
  const b = e.currentTarget;
  b.disabled = true;
  try {
    const r = await api('balance_all', { method: 'POST' });
    toast('额度已刷新 ' + r.ok + ' 个账号', 'ok');
  } catch (err) { toast(err.message, 'err'); }
  b.disabled = false;
  loadAccounts().catch(() => {});
};

/* ── 添加账号 ─────────────────────────────────────────────────────── */
function addOpen() {
  $('addVeil').classList.add('on');
  $('addState').hidden = true;
  setTimeout(() => $('addSession').focus(), 60);
}
function addClose() { $('addVeil').classList.remove('on'); }
$('btnAdd').onclick = addOpen;
$('btnAddClose').onclick = addClose;
$('addVeil').addEventListener('click', e => { if (e.target === $('addVeil')) addClose(); });
document.querySelectorAll('#addTabs .tab').forEach(t => t.onclick = () => {
  document.querySelectorAll('#addTabs .tab').forEach(x => x.classList.toggle('on', x === t));
  $('addTabSession').hidden = t.dataset.tab !== 'session';
  $('addTabJson').hidden = t.dataset.tab !== 'json';
});
function addState(msg, cls) {
  const el = $('addState');
  el.hidden = false;
  el.className = 'state ' + (cls || '');
  el.textContent = msg;
}
$('btnAddSubmit').onclick = async () => {
  const b = $('btnAddSubmit');
  b.disabled = true;
  const isJSON = !$('addTabJson').hidden;
  addState('校验登录态…', 'dots');
  try {
    if (isJSON) {
      const raw = $('addJSON').value.trim();
      if (!raw) throw new Error('先粘贴账号 JSON');
      const r = await api('accounts/import', { method: 'POST', body: raw });
      toast('已导入 ' + r.added + ' 个账号' + (r.failed ? '，失败 ' + r.failed : ''), r.failed ? 'err' : 'ok');
      $('addJSON').value = '';
    } else {
      const session = $('addSession').value.trim();
      if (!session) throw new Error('先粘贴 session');
      const r = await api('accounts/add_session', {
        method: 'POST',
        body: JSON.stringify({ region: $('addRegion').value, session, image_id: $('addImage').value.trim() }),
      });
      toast(r.warning || '已添加 ' + (r.account && r.account.name || ''), r.warning ? 'err' : 'ok');
      $('addSession').value = '';
      $('addImage').value = '';
    }
    addClose();
    loadAccounts().catch(() => {});
  } catch (err) {
    addState(err.message, 'err');
  }
  b.disabled = false;
};

/* ── 模型 ─────────────────────────────────────────────────────────── */
async function loadModels() {
  const d = await api('models');
  const items = d.items || [];
  const stamp = r => (d[r] && d[r].fetched_at ? ago(new Date(d[r].fetched_at * 1000).toISOString()) : '未拉取');
  $('mdNote').textContent = '国内 ' + ((d.cn && d.cn.count) || 0) + ' · 国际 ' + ((d.intl && d.intl.count) || 0)
    + '（更新于 国内 ' + stamp('cn') + ' / 国际 ' + stamp('intl') + '）';
  $('mdBody').innerHTML = items.length ? items.map(m => {
    const marks = [];
    if (m.is_free) marks.push('<span class="tag ok">免费</span>');
    if (m.is_default) marks.push('<span class="tag mute">默认</span>');
    if (m.thinking) marks.push('<span class="tag warn">思考</span>');
    return '<tr>'
      + '<td class="who"><div class="nm">' + esc(m.id) + '</div><div class="id">' + esc(m.name || '') + '</div></td>'
      + '<td>' + (m.region === 'intl' ? '国际版' : '国内版') + '</td>'
      + '<td class="num">' + esc(m.interface_type || '—') + '</td>'
      + '<td class="num">' + esc(m.cli || '—') + '</td>'
      + '<td class="num">' + (m.context_limit ? num(m.context_limit) : '—') + '</td>'
      + '<td class="num">' + (m.output_limit ? num(m.output_limit) : '—') + '</td>'
      + '<td>' + (marks.join(' ') || '<span class="tag mute">—</span>') + '</td>'
      + '</tr>';
  }).join('') : '<tr><td colspan="7"><div class="empty"><div class="big">模型目录为空</div>点「刷新模型清单」从上游拉取（需至少一个可用账号）</div></td></tr>';
}
$('btnMdRefresh').onclick = async e => {
  const b = e.currentTarget;
  b.disabled = true;
  b.textContent = '拉取中…';
  try {
    const r = await api('models/refresh', { method: 'POST' });
    const errs = (r.errors || []).join('；');
    toast('已刷新：国内 ' + r.cn + ' · 国际 ' + r.intl + (errs ? '（' + errs + '）' : ''), errs ? 'err' : 'ok');
  } catch (err) { toast(err.message, 'err'); }
  b.disabled = false;
  b.textContent = '刷新模型清单';
  loadModels().catch(() => {});
};

/* ── 用量 ─────────────────────────────────────────────────────────── */
async function loadUsage() {
  const days = $('usDays').value || '7';
  const [u, rl] = await Promise.all([api('usage?days=' + days), api('request_logs?limit=60')]);
  const rows = u.days || [];
  $('usBody').innerHTML = rows.length ? rows.map(d =>
    '<tr><td class="num">' + esc(d.date) + '</td><td class="num">' + num(d.requests) + '</td>'
    + '<td class="num">' + (d.errors ? '<span class="tag bad">' + num(d.errors) + '</span>' : '0') + '</td>'
    + '<td class="num">' + num(d.chars) + '</td><td class="num">' + num(d.input) + '</td>'
    + '<td class="num">' + num(d.output) + '</td></tr>'
  ).join('') : '<tr><td colspan="6"><div class="empty">还没有请求</div></td></tr>';
  const recs = rl.records || [];
  $('reqNote').textContent = recs.length ? '最近 ' + recs.length + ' 条' : '';
  $('reqBody').innerHTML = recs.length ? recs.map(r => {
    const bad = r.status >= 400 || r.error;
    const t = new Date(r.ts * 1000).toLocaleString('zh-CN', { hour12: false });
    return '<tr><td class="num">' + esc(t) + '</td><td>' + esc(r.protocol) + '</td>'
      + '<td class="num">' + esc(r.model || '—') + '</td><td class="num">' + esc(r.account || '—') + '</td>'
      + '<td class="num">' + (bad ? '<span class="tag bad">' + r.status + '</span>' : '<span class="tag ok">' + r.status + '</span>') + '</td>'
      + '<td class="num">' + num(r.ms) + 'ms</td>'
      + '<td>' + (r.error ? '<span style="color:var(--bad)">' + esc(r.error) + '</span>' : '') + '</td></tr>';
  }).join('') : '<tr><td colspan="7"><div class="empty">还没有请求</div></td></tr>';
}
$('usDays').onchange = () => loadUsage().catch(e => toast(e.message, 'err'));

/* ── 运行日志 ─────────────────────────────────────────────────────── */
async function loadLogs() {
  const d = await api('logs');
  const box = $('logBox');
  const entries = (d.entries || []).filter(e => !logCh || e.ch === logCh);
  const chName = { chat: '对话', task: '任务', sys: '系统' };
  box.innerHTML = entries.length ? entries.map(e => {
    const cls = /error|失败|ERR|panic/i.test(e.text) ? 'e' : (/WARN|重试|冷却/.test(e.text) ? 'w' : '');
    const t = new Date(e.ts).toLocaleTimeString('zh-CN', { hour12: false });
    return '<span class="ln ' + cls + '"><em class="lch c-' + esc(e.ch) + '">' + esc(chName[e.ch] || e.ch) + '</em>'
      + esc(t) + ' ' + esc(e.text) + '</span>';
  }).join('') : '<span class="ln">暂无日志</span>';
  $('logNote').textContent = entries.length + ' 行';
  if (logPin) box.scrollTop = box.scrollHeight;
}
document.querySelectorAll('#logChips .chip').forEach(c => c.onclick = () => {
  document.querySelectorAll('#logChips .chip').forEach(x => x.classList.toggle('on', x === c));
  logCh = c.dataset.ch;
  loadLogs().catch(() => {});
});
$('logPin').onchange = e => { logPin = e.target.checked; };

/* ── 配置 ─────────────────────────────────────────────────────────── */
function fillCfg(c) {
  $('cfListen').value = c.listen || '';
  $('cfKey').value = c.api_key || '';
  $('cfAuthDir').value = c.auth_dir || '';
  $('cfDataDir').value = c.data_dir || '';
  $('cfHardQuota').value = (c.cooldown && c.cooldown.hard_quota) || '';
  $('cfSoftRate').value = (c.cooldown && c.cooldown.soft_rate) || '';
  $('cfErrCool').value = (c.cooldown && c.cooldown.err_cooldown) || '';
  $('cfErrThreshold').value = (c.cooldown && c.cooldown.err_threshold) || 5;
  const s = c.schedule || {};
  $('cfCheckinEnabled').checked = !!s.checkin_enabled;
  $('cfCheckinHours').value = (s.checkin_hours || []).join(',');
  $('cfQuotaHour').value = s.quota_check_hour != null ? s.quota_check_hour : 0;
  $('cfQuotaRefresh').value = s.quota_refresh_minutes != null ? s.quota_refresh_minutes : 30;
  const u = c.upstream || {};
  $('cfTimeout').value = u.timeout_seconds || 30;
  $('cfIdle').value = u.idle_timeout_seconds || 300;
  $('cfTaskTimeout').value = u.task_timeout_seconds || 7200;
  $('cfUA').value = u.user_agent || '';
  $('cfNote').textContent = c.api_key_masked ? '密钥已脱敏显示；不改动请保持原样。' : '';
}
async function loadConfig() {
  const d = await api('config');
  cfgLoaded = d.config;
  fillCfg(d.config);
}
$('btnCfSave').onclick = async e => {
  const b = e.currentTarget;
  const hours = $('cfCheckinHours').value.split(',').map(s => parseInt(s.trim(), 10)).filter(n => n >= 0 && n <= 23);
  const body = {
    listen: $('cfListen').value.trim(),
    api_key: $('cfKey').value.trim(),
    auth_dir: $('cfAuthDir').value.trim(),
    data_dir: $('cfDataDir').value.trim(),
    cooldown: {
      hard_quota: $('cfHardQuota').value.trim(),
      soft_rate: $('cfSoftRate').value.trim(),
      err_cooldown: $('cfErrCool').value.trim(),
      err_threshold: parseInt($('cfErrThreshold').value, 10) || 5,
    },
    schedule: {
      checkin_enabled: $('cfCheckinEnabled').checked,
      checkin_hours: hours,
      quota_check_hour: parseInt($('cfQuotaHour').value, 10) || 0,
      quota_refresh_minutes: parseInt($('cfQuotaRefresh').value, 10) || 30,
    },
    upstream: {
      timeout_seconds: parseInt($('cfTimeout').value, 10) || 30,
      idle_timeout_seconds: parseInt($('cfIdle').value, 10) || 300,
      task_timeout_seconds: parseInt($('cfTaskTimeout').value, 10) || 7200,
      user_agent: $('cfUA').value.trim(),
    },
    panel: (cfgLoaded && cfgLoaded.panel) || { enabled: true },
  };
  b.disabled = true;
  try {
    const r = await api('config', { method: 'POST', body: JSON.stringify(body) });
    toast(r.restart_required && r.restart_required.length
      ? '已保存；需重启生效：' + r.restart_required.join('、')
      : '已保存', 'ok');
    loadConfig().catch(() => {});
  } catch (err) { toast(err.message, 'err'); }
  b.disabled = false;
};

/* ── 启动 / 轮询 ──────────────────────────────────────────────────── */
function start() {
  const first = (location.hash || '#accounts').slice(1);
  go(TITLES[first] ? first : 'accounts');
  loadAccounts().catch(() => {});
  clearInterval(refTimer);
  // 账号池 5 秒轮询（池状态变化快）；其余视图只在进入时拉取。
  refTimer = setInterval(() => {
    if (document.hidden) return;
    if (view === 'accounts') loadAccounts().catch(() => {});
    if (view === 'logs') loadLogs().catch(() => {});
  }, 5000);
}
$('btnRefresh').onclick = () => {
  const load = view === 'accounts' ? loadAccounts : view === 'usage' ? loadUsage : view === 'models' ? loadModels : view === 'config' ? loadConfig : loadLogs;
  load().then(() => toast('已刷新', 'ok')).catch(e => toast(e.message, 'err'));
};
/* 首次进入延到本轮脚本求值之后再 go()：go() 会同步触发视图的数据加载，
   而那些加载函数依赖后面声明的模块级状态（避免 TDZ）。 */
addEventListener('load', () => {
  if (!localStorage.getItem(LS_KEY)) { openKey(); return; }
  start();
});
