const API = '/api/v1';
let services = [];
let mailMonitors = [];
let mailHistory = {};
let serviceHistory = {};
let editingId = null;
let viewMode = localStorage.getItem('uptime-view-mode-v2') || 'table';
let catalogPeriod = '6h';
const $ = id => document.getElementById(id);

const checkTypeHints = {
  http: 'HTTP или HTTPS: http://example.com/health либо https://example.com/health (порты 80/443)',
  smtp: 'SMTP или SMTPS: smtp://mail.example.com:25 либо smtps://mail.example.com:465',
  imap: 'IMAP или IMAPS: imap://mail.example.com:143 либо imaps://mail.example.com:993',
  pop3: 'POP3 или POP3S: pop3://mail.example.com:110 либо pop3s://mail.example.com:995',
  mysql: 'Пример: mysql://db.example.com:3306',
  redis: 'Redis или Redis TLS: redis://redis.example.com:6379 либо rediss://redis.example.com:6380',
  rabbitmq: 'RabbitMQ/AMQP: amqp://rabbit.example.com:5672 либо amqps://rabbit.example.com:5671',
  postgres: 'PostgreSQL: postgres://db.example.com:5432 или postgresql://db.example.com:5432?sslmode=require'
};

const checkTypeOptions = [
  ['http', 'HTTP(s)'], ['smtp', 'SMTP(s)'], ['imap', 'IMAP(s)'], ['pop3', 'POP3(s)'],
  ['mysql', 'MySQL'], ['redis', 'Redis(s)'], ['rabbitmq', 'RabbitMQ / AMQP(s)'], ['postgres', 'PostgreSQL']
];
const capabilityLabels = { http: 'HTTP(s)', smtp: 'SMTP(s)', imap: 'IMAP(s)', pop3: 'POP3(s)', mysql: 'MySQL', redis: 'Redis(s)', rabbitmq: 'RabbitMQ / AMQP(s)', postgres: 'PostgreSQL' };

const checkTypeHint = document.createElement('small');
checkTypeHint.className = 'field-hint';
checkTypeHint.id = 'checkTypeHint';
document.addEventListener('DOMContentLoaded', () => {
  $('checkType').innerHTML = checkTypeOptions.map(([value, label]) => `<option value="${value}">${label}</option>`).join('');
  $('url').after(checkTypeHint);
  $('checkType').onchange = updateCheckTypeHint;
  updateCheckTypeHint();
});
function updateCheckTypeHint() {
  if ($('checkTypeHint') && $('checkType')) $('checkTypeHint').textContent = checkTypeHints[$('checkType').value] || '';
}
function canonicalCheckType(value) {
  return ({ https: 'http', smtps: 'smtp', imaps: 'imap', pop3s: 'pop3', rediss: 'redis', amqp: 'rabbitmq', amqps: 'rabbitmq', postgresql: 'postgres' })[value] || value || 'http';
}

async function api(path, options = {}) {
  const response = await fetch(API + path, { headers: { 'Content-Type': 'application/json' }, ...options });
  if (!response.ok) throw new Error(await response.text() || response.statusText);
  return response.status === 204 ? null : response.json();
}
function escapeHtml(value) { return String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c])); }
function statusLabel(check) { if (!check) return ['offline', 'Нет проверки']; return [check.status, check.status === 'online' ? 'Работает' : check.status === 'degraded' ? 'Проблемы' : check.status === 'tls_error' ? 'TLS error' : 'Недоступен']; }
function statusIndicator(status, label) { const tone = status === 'online' ? 'green' : (status === 'degraded' || status === 'tls_error' ? 'orange' : status === 'offline' ? 'red' : 'gray'); return `<span class="status-indicator status-indicator-${tone}" title="${escapeHtml(label)}" aria-label="${escapeHtml(label)}"></span>`; }
function statusDroplet(status, label) { const tone = status === 'online' ? 'green' : (status === 'degraded' || status === 'tls_error' ? 'orange' : status === 'offline' ? 'red' : 'gray'); return `<span class="status-droplet status-droplet-${tone}" title="${escapeHtml(label)}" aria-label="${escapeHtml(label)}"><svg viewBox="0 0 24 28" fill="currentColor" aria-hidden="true"><path d="M12 1S3 11 3 17a9 9 0 0 0 18 0C21 11 12 1 12 1Z"/></svg></span>`; }
function currentStatus(service) { return service.last_check ? service.last_check.status : 'offline'; }
function capabilityClass(value) { return String(value || 'http').toLowerCase().replace(/[^a-z0-9_-]/g, ''); }
function serviceUptime(service) {
  const checks = serviceHistory[service.id] || [];
  if (!checks.length) return null;
  const onlineChecks = checks.filter(check => check.status === 'online').length;
  return (onlineChecks / checks.length * 100).toFixed(2).replace(/\.00$/, '');
}
function uptimeTone(value) {
  if (value === null) return 'empty';
  const percentage = Number(value);
  return percentage >= 99.9 ? 'online' : percentage > 0 ? 'degraded' : 'offline';
}
function uptimeBadgeSVG(value, label = 'Uptime') {
  const text = value === null ? '—' : `${value}%`;
  const tone = uptimeTone(value);
  const palette = {
    online: ['#4fd98a', '#07140d'],
    degraded: ['#e4b34b', '#211804'],
    offline: ['#e7354b', '#ffffff'],
    empty: ['#303640', '#a1a7b1']
  }[tone];
  const width = text.length > 5 ? 78 : 66;
  return `<svg class="uptime-svg-badge" width="${width}" height="28" viewBox="0 0 ${width} 28" role="img" aria-label="${escapeHtml(label)} ${escapeHtml(text)}"><title>${escapeHtml(label)}: ${escapeHtml(text)}</title><rect x="1" y="1" width="${width - 2}" height="26" rx="13" fill="${palette[0]}"/><text x="${width / 2}" y="19" text-anchor="middle" fill="${palette[1]}" font-family="Inter, system-ui, sans-serif" font-size="12" font-weight="750">${escapeHtml(text)}</text></svg>`;
}
function formatStatusDuration(service) {
  const latest = service.last_check;
  if (!latest) return 'Ожидает первого запуска';
  const checks = [...(serviceHistory[service.id] || [])].sort((a, b) => new Date(a.checked_at) - new Date(b.checked_at));
  let startedAt = new Date(latest.checked_at);
  for (let index = checks.length - 1; index >= 0; index -= 1) {
    if (checks[index].status !== latest.status) break;
    startedAt = new Date(checks[index].checked_at);
  }
  const seconds = Math.max(0, Math.floor((Date.now() - startedAt.getTime()) / 1000));
  if (seconds < 60) return `${seconds} сек.`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes} мин.`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} ч. ${minutes % 60} мин.`;
  return `${Math.floor(hours / 24)} дн. ${hours % 24} ч.`;
}
function serviceHistoryBars(service) {
  const checks = [...(serviceHistory[service.id] || [])].sort((a, b) => new Date(a.checked_at) - new Date(b.checked_at)).slice(-48);
  const bars = Array.from({ length: 48 }, (_, index) => {
    const check = checks[index - (48 - checks.length)];
    const status = check?.status || 'empty';
    const title = check ? `${check.status} · ${new Date(check.checked_at).toLocaleString()}` : 'Нет проверки';
    return `<i class="table-history-bar history-${escapeHtml(status)}" title="${escapeHtml(title)}"></i>`;
  }).join('');
  return `<div class="table-history-bars">${bars}</div><div class="table-history-scale"><span>48 проверок назад</span><span>Сейчас</span></div>`;
}
function serviceHTTPStatus(check) {
  return check?.http_status ? `HTTP ${check.http_status}` : '—';
}
function serviceCertificate(check) {
  if (!check?.tls) return '—';
  if (!check.tls.expires_at) return escapeHtml(check.tls.status || '—');
  const date = new Date(check.tls.expires_at).toLocaleDateString();
  return `${date}<small>${Number.isFinite(check.tls.days_remaining) ? `через ${check.tls.days_remaining} дн.` : ''}</small>`;
}

function renderServiceSidebar() {
  const root = $('serviceSidebarList');
  if (!services.length) {
    root.innerHTML = '<div class="empty">Сервисов нет</div>';
    return;
  }
  root.innerHTML = services.map(service => {
    const checks = (serviceHistory[service.id] || []).slice(-30);
    const uptime = serviceUptime(service) || '0';
    const status = currentStatus(service);
    const statusText = status === 'online' ? 'Работает' : status === 'degraded' ? 'Проблемы' : 'Недоступен';
    const bars = Array.from({ length: 24 }, (_, index) => {
      const check = checks[checks.length - 24 + index];
      const barStatus = check ? check.status : 'empty';
      return `<i class="history-bar history-${escapeHtml(barStatus)}" title="${check ? `${escapeHtml(check.status)} · ${new Date(check.checked_at).toLocaleString()}` : 'Нет проверки'}"></i>`;
    }).join('');
    return `<button class="sidebar-service sidebar-${escapeHtml(status)}" data-sidebar-service="${service.id}"><span class="sidebar-service-top">${uptimeBadgeSVG(uptime, 'Доступность')}<span class="sidebar-status">${escapeHtml(statusText)}</span></span><span class="sidebar-service-name">${escapeHtml(service.name)}</span><span class="sidebar-bars">${bars}</span></button>`;
  }).join('');
  root.querySelectorAll('[data-sidebar-service]').forEach(button => {
    button.onclick = () => openDetail(Number(button.dataset.sidebarService));
  });
}

function renderMailSidebar() {
  const root = $('mailSidebarList');
  if (!mailMonitors.length) {
    root.innerHTML = '<div class="sidebar-empty">Нет проверок почты</div>';
    return;
  }
  root.innerHTML = mailMonitors.map(monitor => {
    const status = monitor.enabled ? (monitor.last_status || 'pending') : 'disabled';
    const statusText = { ok: 'Работает', warning: 'Проблемы', error: 'Ошибка', not_found: 'Не найдено', pending: 'Ожидает', disabled: 'Выключена' }[status] || status;
    const statusClass = status === 'ok' ? 'online' : (status === 'pending' || status === 'disabled' ? 'pending' : 'degraded');
    const checks = (mailHistory[monitor.domain] || []).slice(0, 30).reverse();
    const bars = Array.from({ length: 30 }, (_, index) => {
      const check = checks[index - (30 - checks.length)];
      const barStatus = check ? check.status : 'empty';
      const title = check ? `${check.status} · ${new Date(check.checked_at).toLocaleString()} · ${check.score}/100` : 'Нет проверки';
      return `<i class="history-bar history-mail-${escapeHtml(barStatus)}" title="${escapeHtml(title)}"></i>`;
    }).join('');
    return `<a class="sidebar-mail sidebar-${statusClass}" href="/uptime/domain?domain=${encodeURIComponent(monitor.domain)}"><span class="sidebar-service-top"><span class="mail-score-badge">${monitor.last_status ? `${escapeHtml(monitor.last_score)}/100` : '—'}</span><span class="sidebar-status">${escapeHtml(statusText)}</span></span><span class="sidebar-service-name">${escapeHtml(monitor.domain)}</span><span class="sidebar-mail-bars">${bars}</span><span class="sidebar-mail-meta">30 дней · каждые ${escapeHtml(monitor.check_interval)}</span></a>`;
  }).join('');
}

function renderServices() {
  if (viewMode === 'table') return renderServiceTable();
  const root = $('serviceList');
  if (!services.length) { root.innerHTML = '<div class="empty">Сервисов по выбранным фильтрам нет.</div>'; return; }
  root.innerHTML = services.map(service => {
    const [status, label] = statusLabel(service.last_check);
    const check = service.last_check;
    const uptime = serviceUptime(service);
    const uptimeBadge = uptimeBadgeSVG(uptime);
    const checkType = canonicalCheckType(service.check_type);
    const capability = `<span class="capability-badge capability-${capabilityClass(checkType)}"><span aria-hidden="true">◈</span> ${escapeHtml(capabilityLabels[checkType] || checkType)}</span>`;
    const tags = (service.tags || []).map(tag => `<span class="badge">${escapeHtml(tag)}</span>`).join('');
    const tls = check?.tls ? `<span class="badge tls-${escapeHtml(check.tls.status)}">TLS: ${escapeHtml(check.tls.status)}${check.tls.days_remaining ? `, ${check.tls.days_remaining} дн.` : ''}${check.tls.expires_at ? ` · до ${new Date(check.tls.expires_at).toLocaleDateString()}` : ''}</span>` : '';
     return `<article class="service"><div><div class="service-title-line">${statusDroplet(status, label)}<div><h3>${escapeHtml(service.name)}</h3><a href="${escapeHtml(service.url)}" target="_blank" rel="noreferrer">${escapeHtml(service.url)}</a></div></div><div class="service-meta">${uptimeBadge}${capability}<span class="badge">${escapeHtml(service.environment)}</span><span class="badge">каждые ${escapeHtml(service.check_interval || '30s')}</span>${service.repeat_notifications_enabled ? `<span class="badge">повтор каждые ${escapeHtml(service.repeat_notification_interval || '5m')}</span>` : ''}${tls}${tags}</div>${certificateSummary(check)}${diagnosticsSummary(check)}<div class="muted">${escapeHtml(service.description || '')}${check ? ` · ${check.response_time_ms} ms` : ''}</div></div>${serviceActionButtons(service)}</article>`;
  }).join('');
  root.querySelectorAll('[data-detail]').forEach(b => b.onclick = () => openDetail(Number(b.dataset.detail)));
  root.querySelectorAll('[data-check]').forEach(b => b.onclick = () => checkService(Number(b.dataset.check), b));
  root.querySelectorAll('[data-edit]').forEach(b => b.onclick = () => openEdit(Number(b.dataset.edit)));
  root.querySelectorAll('[data-delete]').forEach(b => b.onclick = () => deleteService(Number(b.dataset.delete)));
}
function applyViewMode() {
  document.body.classList.toggle('catalog-table-mode', viewMode === 'table');
  $('cardView').classList.toggle('primary', viewMode === 'cards');
  $('tableView').classList.toggle('primary', viewMode === 'table');
}
function serviceActionButtons(service) {
  return `<div class="service-actions" aria-label="Действия с сервисом"><button class="icon-button" data-detail="${service.id}" title="Подробнее" aria-label="Подробнее"><span aria-hidden="true">${actionIcon('details')}</span></button><button class="icon-button" data-check="${service.id}" title="Проверить сейчас" aria-label="Проверить сейчас"><span aria-hidden="true">${actionIcon('check')}</span></button><button class="icon-button" data-edit="${service.id}" title="Изменить" aria-label="Изменить"><span aria-hidden="true">${actionIcon('edit')}</span></button><button class="icon-button danger" data-delete="${service.id}" title="Удалить" aria-label="Удалить"><span aria-hidden="true">${actionIcon('delete')}</span></button></div>`;
}
function actionIcon(name) {
  const paths = {
    details: '<circle cx="11" cy="11" r="7"/><path d="m20 20-4-4"/>',
    check: '<path d="M20 11a9 9 0 1 1-2.64-6.36"/><path d="M20 4v7h-7"/>',
    edit: '<path d="M12 20h9"/><path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L8 18l-4 1 1-4Z"/>',
    delete: '<path d="M3 6h18"/><path d="M8 6V4h8v2"/><path d="M19 6l-1 14H6L5 6"/><path d="M10 11v5M14 11v5"/>'
  };
  return `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">${paths[name]}</svg>`;
}
function bindServiceActions(root) {
  root.querySelectorAll('[data-detail]').forEach(b => b.onclick = () => openDetail(Number(b.dataset.detail)));
  root.querySelectorAll('[data-check]').forEach(b => b.onclick = () => checkService(Number(b.dataset.check), b));
  root.querySelectorAll('[data-edit]').forEach(b => b.onclick = () => openEdit(Number(b.dataset.edit)));
  root.querySelectorAll('[data-delete]').forEach(b => b.onclick = () => deleteService(Number(b.dataset.delete)));
}
function renderServiceTable() {
  const root = $('serviceList');
  if (!services.length) { root.innerHTML = '<div class="empty">Сервисов по выбранным фильтрам нет.</div>'; return; }
   root.innerHTML = `<div class="service-table-wrap"><table class="service-table uptime-catalog"><thead><tr><th class="status-col" aria-label="Статус"></th><th>SITE (${services.length})</th><th>TYPE</th><th>STATUS</th><th class="period-heading"><button data-catalog-period="6h" class="${catalogPeriod === '6h' ? 'active' : ''}">6h</button><button data-catalog-period="24h" class="${catalogPeriod === '24h' ? 'active' : ''}">24h</button><button data-catalog-period="7d" class="${catalogPeriod === '7d' ? 'active' : ''}">7d</button></th><th>UPTIME</th><th>RESPONSE</th><th>HTTP</th><th>CERT</th><th class="actions-heading" aria-label="Действия"></th></tr></thead><tbody>${services.map(service => {
    const [status, label] = statusLabel(service.last_check);
    const check = service.last_check;
    const type = canonicalCheckType(service.check_type);
    const state = check ? label : 'Pending';
    const timing = check ? `${check.response_time_ms} ms` : 'Ожидает первого запуска';
     return `<tr class="service-row" data-service-row="${service.id}"><td class="status-col">${statusDroplet(status, label)}</td><td class="site-cell"><strong>${escapeHtml(service.name)}</strong><a class="table-url" href="${escapeHtml(service.url)}" target="_blank" rel="noreferrer">${escapeHtml(service.url)} ↗</a></td><td><span class="type-label">${escapeHtml(capabilityLabels[type] || type)}</span><small>${escapeHtml(service.check_interval || '30s')} · ${escapeHtml(timing)}</small></td><td><strong class="table-status status-${escapeHtml(status)}">${escapeHtml(state)}</strong><small>${escapeHtml(formatStatusDuration(service))}</small></td><td class="row-period-label">${catalogPeriod}</td><td class="table-uptime"><div>${serviceHistoryBars(service)}</div>${uptimeBadgeSVG(serviceUptime(service))}</td><td class="metrics-cell">${check ? `${escapeHtml(check.response_time_ms)} ms` : '<em>Ожидание</em>'}</td><td class="metrics-cell">${serviceHTTPStatus(check)}</td><td class="metrics-cell table-cert">${serviceCertificate(check)}</td><td class="actions-cell">${serviceActionButtons(service)}</td></tr><tr class="service-statistics"><td colspan="10"><div class="expanded-metrics" data-metrics-root="${service.id}"><span class="muted">Загрузка статистики…</span></div></td></tr>`;
  }).join('')}</tbody></table></div>`;
  bindServiceActions(root);
  root.querySelectorAll('[data-metrics-root]').forEach(metricsRoot => {
    loadExpandedMetrics(Number(metricsRoot.dataset.metricsRoot), metricsRoot, catalogPeriod);
  });
  root.querySelectorAll('[data-catalog-period]').forEach(button => button.onclick = async () => {
    catalogPeriod = button.dataset.catalogPeriod;
    root.querySelectorAll('[data-catalog-period]').forEach(periodButton => periodButton.classList.toggle('active', periodButton === button));
    root.querySelectorAll('.row-period-label').forEach(label => { label.textContent = catalogPeriod; });
    root.querySelectorAll('[data-metrics-root]').forEach(metricsRoot => loadExpandedMetrics(Number(metricsRoot.dataset.metricsRoot), metricsRoot, catalogPeriod));
  });
}
async function loadExpandedMetrics(id, root, period) {
  root.innerHTML = '<span class="muted">Загрузка статистики…</span>';
  try {
    const metrics = await api(`/services/${id}/metrics?period=${period}`);
    root.innerHTML = `${renderMetricsPanel(metrics)}<div class="metrics-footer"><span>${metrics.buckets.length} points · ${escapeHtml(period)}</span><button type="button" class="full-statistics" data-full-statistics="${id}">Full statistics</button></div>`;
    root.querySelectorAll('[data-period]').forEach(button => { button.onclick = () => loadExpandedMetrics(id, root, button.dataset.period); });
    root.querySelector('[data-full-statistics]').onclick = () => openDetail(id);
  } catch (error) {
    root.innerHTML = `<span class="muted">Не удалось загрузить статистику: ${escapeHtml(error.message)}</span>`;
  }
}
function certificateSummary(check) {
  if (!check?.tls) return '';
  const tls = check.tls;
  const expiry = tls.expires_at ? new Date(tls.expires_at).toLocaleString() : 'не определён';
  const days = Number.isFinite(tls.days_remaining) ? `${tls.days_remaining} дн.` : '—';
  return `<div class="certificate-summary"><strong>Сертификат:</strong> ${escapeHtml(tls.status)} · <strong>до:</strong> ${escapeHtml(expiry)} · <strong>осталось:</strong> ${escapeHtml(days)}${tls.hostname ? ` · ${escapeHtml(tls.hostname)}` : ''}${tls.error ? ` · ${escapeHtml(tls.error)}` : ''}</div>`;
}
function diagnosticsSummary(check) {
  if (!check?.diagnostics) return '';
  const details = Object.entries(check.diagnostics).map(([key, value]) => `${key}: ${typeof value === 'object' ? JSON.stringify(value) : value}`).join(' · ');
  return details ? `<div class="diagnostics-summary"><strong>Диагностика:</strong> ${escapeHtml(details)}</div>` : '';
}
function renderDashboard(dashboard) {
  ['total', 'online', 'degraded', 'offline'].forEach(key => $(key).textContent = dashboard[key] || 0);
  $('recentErrors').innerHTML = (dashboard.recent_errors || []).map(check => `<div class="list-row"><span class="badge status-${escapeHtml(check.status)}">${escapeHtml(check.status)}</span><span>${escapeHtml(check.error || 'HTTP ' + check.http_status)}</span><small>${new Date(check.checked_at).toLocaleString()}</small></div>`).join('') || '<div class="muted">Ошибок нет</div>';
  const points = dashboard.response_times || [];
  const max = Math.max(...points.map(p => p.milliseconds), 1);
  $('responseTimes').innerHTML = points.map(point => `<div class="bar-row"><span>${escapeHtml(point.service_name)}</span><div class="bar"><i style="width:${Math.max(3, point.milliseconds / max * 100)}%"></i></div><small>${point.milliseconds} ms</small></div>`).join('') || '<div class="muted">Нет данных</div>';
}
function renderCheckChart(checks) {
  const ordered = [...(checks || [])].reverse();
  if (!ordered.length) return '<div class="muted">Пока нет данных для графика</div>';
  const width = 760;
  const height = 150;
  const padding = 24;
  const step = ordered.length === 1 ? 0 : (width - padding * 2) / (ordered.length - 1);
  const y = { online: 35, degraded: 75, tls_error: 75, offline: 115 };
  const color = { online: '#16845b', degraded: '#b7791f', tls_error: '#8b3b9c', offline: '#d34242' };
  const points = ordered.map((check, index) => ({ check, x: padding + index * step, y: y[check.status] || y.offline, color: color[check.status] || color.offline }));
  const lines = points.slice(1).map((point, index) => `<line x1="${points[index].x}" y1="${points[index].y}" x2="${point.x}" y2="${point.y}" stroke="#454b56" stroke-width="2" />`).join('');
  const circles = points.map(point => `<circle cx="${point.x}" cy="${point.y}" r="6" fill="${point.color}"><title>${escapeHtml(point.check.status)} · ${new Date(point.check.checked_at).toLocaleString()} · ${point.check.response_time_ms} ms${point.check.error ? ` · ${escapeHtml(point.check.error)}` : ''}</title></circle>`).join('');
  return `<div class="check-chart"><svg viewBox="0 0 ${width} ${height}" role="img" aria-label="График статусов проверок"><line x1="${padding}" y1="35" x2="${width - padding}" y2="35" stroke="#2f353e" /><line x1="${padding}" y1="75" x2="${width - padding}" y2="75" stroke="#2f353e" /><line x1="${padding}" y1="115" x2="${width - padding}" y2="115" stroke="#2f353e" />${lines}${circles}</svg><div class="chart-legend"><span class="legend-online">● online</span><span class="legend-degraded">● degraded</span><span class="legend-offline">● offline</span></div></div>`;
}
function metricAverage(metrics, field) {
  const total = metrics.total_checks || 0;
  if (!total) return 0;
  return Math.round(metrics.buckets.reduce((sum, bucket) => sum + Number(bucket[field] || 0), 0) / total);
}
function metricBars(buckets, value, color) {
  const values = buckets.map(bucket => Number(bucket[value] || 0));
  const max = Math.max(...values, 1);
  return values.map((item, index) => `<i class="metric-bar" style="height:${Math.max(4, item / max * 100)}%;background:${color}" title="${escapeHtml(buckets[index].bucket)}: ${item}"></i>`).join('');
}
function metricLineChart(buckets, series) {
  if (!buckets.length) return '<div class="metric-line-empty">Нет данных</div>';
  const width = 560;
  const height = 180;
  const left = 42;
  const right = 12;
  const top = 12;
  const bottom = 28;
  const values = series.map(item => buckets.map(bucket => {
    const checks = Number(bucket.total_checks || 0);
    return checks > 0 ? Number(bucket[item.field] || 0) / checks : 0;
  }));
  const max = Math.max(...values.flat(), 1);
  const step = buckets.length === 1 ? 0 : (width - left - right) / (buckets.length - 1);
  const y = value => height - bottom - (value / max) * (height - top - bottom);
  const grid = [0, 0.5, 1].map(fraction => { const value = max * fraction; const lineY = y(value); return `<line x1="${left}" y1="${lineY}" x2="${width - right}" y2="${lineY}" /><text x="${left - 6}" y="${lineY + 4}" text-anchor="end">${Math.round(value)} ms</text>`; }).join('');
  const lines = values.map((items, seriesIndex) => {
    const points = items.map((value, index) => `${left + index * step},${y(value)}`).join(' ');
    const dots = items.map((value, index) => `<circle cx="${left + index * step}" cy="${y(value)}" r="3"><title>${escapeHtml(series[seriesIndex].label)}: ${Math.round(value)} ms</title></circle>`).join('');
    return `<polyline points="${points}" stroke="${series[seriesIndex].color}" />${dots}`;
  }).join('');
  const labelIndexes = [...new Set([0, Math.floor((buckets.length - 1) / 2), buckets.length - 1])];
  const labels = labelIndexes.map(index => `<text x="${left + index * step}" y="${height - 7}" text-anchor="middle">${escapeHtml(String(buckets[index].bucket || ''))}</text>`).join('');
  const legend = series.map(item => `<span><i style="background:${item.color}"></i>${escapeHtml(item.label)}</span>`).join('');
  return `<div class="metric-line-chart"><svg viewBox="0 0 ${width} ${height}" role="img" aria-label="DNS, Connect и TLS по времени">${grid}${lines}${labels}</svg><div class="metric-line-legend">${legend}</div></div>`;
}
function formatBytes(value) {
  const bytes = Math.max(0, Number(value) || 0);
  const units = ['байт', 'КБ', 'МБ', 'ГБ', 'ТБ'];
  if (bytes < 1024) return `${Math.round(bytes)} байт`;
  const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const amount = bytes / (1024 ** exponent);
  const precision = amount >= 100 ? 0 : amount >= 10 ? 1 : 2;
  return `${Number(amount.toFixed(precision))} ${units[exponent]}`;
}
function formatMetricValue(value, unit) {
  return unit === 'bytes' ? formatBytes(value) : `${Math.round(value)} ${unit}`;
}
function metricBarChart(buckets, field, color, unit) {
  if (!buckets.length) return '<div class="metric-line-empty">Нет данных</div>';
  const width = 560;
  const height = 180;
  const left = 42;
  const right = 12;
  const top = 12;
  const bottom = 28;
  const values = buckets.map(bucket => {
    const checks = Number(bucket.total_checks || 0);
    return checks > 0 ? Number(bucket[field] || 0) / checks : 0;
  });
  const max = Math.max(...values, 1);
  const step = (width - left - right) / values.length;
  const barWidth = Math.max(3, step * 0.72);
  const y = value => height - bottom - (value / max) * (height - top - bottom);
  const grid = [0, 0.5, 1].map(fraction => { const value = max * fraction; const lineY = y(value); return `<line x1="${left}" y1="${lineY}" x2="${width - right}" y2="${lineY}" /><text x="${left - 6}" y="${lineY + 4}" text-anchor="end">${escapeHtml(formatMetricValue(value, unit))}</text>`; }).join('');
  const bars = values.map((value, index) => { const x = left + index * step + (step - barWidth) / 2; return `<rect x="${x}" y="${y(value)}" width="${barWidth}" height="${Math.max(1, height - bottom - y(value))}" fill="${color}"><title>${escapeHtml(formatMetricValue(value, unit))}</title></rect>`; }).join('');
  const labelIndexes = [...new Set([0, Math.floor((buckets.length - 1) / 2), buckets.length - 1])];
  const labels = labelIndexes.map(index => `<text x="${left + index * step + step / 2}" y="${height - 7}" text-anchor="middle">${escapeHtml(String(buckets[index].bucket || ''))}</text>`).join('');
  return `<div class="metric-line-chart"><svg viewBox="0 0 ${width} ${height}" role="img" aria-label="График ${escapeHtml(unit === 'bytes' ? 'размера страницы' : unit)}">${grid}${bars}${labels}</svg></div>`;
}
function renderMetricsPanel(metrics) {
  const avgResponse = metricAverage(metrics, 'response_sum_ms');
  const avgPageSize = metricAverage(metrics, 'page_size_sum_bytes');
  return `<section class="service-metrics"><div class="metrics-toolbar"><strong>Статистика</strong><div>${['6h', '24h', '7d', '30d'].map(period => `<button type="button" class="metric-period ${metrics.period === period ? 'primary' : ''}" data-period="${period}">${period}</button>`).join('')}</div></div><div class="metric-summary"><div><strong>${metrics.uptime_percent.toFixed(2)}%</strong><span>Uptime</span></div><div><strong>${metrics.total_checks}</strong><span>Проверок</span></div><div><strong>${metrics.online_checks}</strong><span>Online</span></div><div><strong>${avgResponse} ms</strong><span>Средний response</span></div></div><div class="metric-panels"><article><h4>RESPONSE TIME</h4>${metricBarChart(metrics.buckets, 'response_sum_ms', '#9e96ff', 'ms')}<small>Среднее: ${avgResponse} ms · максимум: ${Math.max(...metrics.buckets.map(bucket => bucket.response_max_ms || 0), 0)} ms</small></article><article><h4>DNS + CONNECT + TLS</h4>${metricLineChart(metrics.buckets, [{ field: 'dns_sum_ms', label: 'DNS', color: '#78b7ff' }, { field: 'connect_sum_ms', label: 'Connect', color: '#f0bd51' }, { field: 'tls_sum_ms', label: 'TLS', color: '#55df91' }])}<small>Средние значения по проверкам</small></article><article><h4>PAGE SIZE</h4>${metricBarChart(metrics.buckets, 'page_size_sum_bytes', '#55df91', 'bytes')}<small>Средний размер: ${formatBytes(avgPageSize)}</small></article></div></section>`;
}
async function loadServiceMetrics(id, period = '24h') {
  const root = $('serviceMetrics');
  if (!root) return;
  try {
    const metrics = await api(`/services/${id}/metrics?period=${period}`);
    root.innerHTML = renderMetricsPanel(metrics);
    root.querySelectorAll('[data-period]').forEach(button => { button.onclick = () => loadServiceMetrics(id, button.dataset.period); });
  } catch (error) {
    root.innerHTML = `<div class="muted">Не удалось загрузить статистику: ${escapeHtml(error.message)}</div>`;
  }
}

function renderReminders(items) {
  if (!$('reminderList')) return;
  $('reminderList').innerHTML = items.map(item => `<div class="list-row ${item.completed ? 'completed' : ''}"><span><strong>${escapeHtml(item.title)}</strong><small>${new Date(item.due_at).toLocaleString()} · ${escapeHtml(services.find(s => s.id === item.service_id)?.name || 'сервис')}</small></span><button data-reminder-delete="${item.id}">Удалить</button></div>`).join('') || '<div class="muted">Напоминаний нет</div>';
  $('reminderList').querySelectorAll('[data-reminder-delete]').forEach(b => b.onclick = async () => { await api(`/reminders/${b.dataset.reminderDelete}`, { method: 'DELETE' }); await load(); });
}
async function load() {
  const query = new URLSearchParams();
  if ($('environmentFilter').value) query.set('environment', $('environmentFilter').value);
  if ($('serviceSearch').value.trim()) query.set('search', $('serviceSearch').value.trim());
  if ($('typeFilter').value) query.set('check_type', $('typeFilter').value);
  if ($('tagFilter').value.trim()) query.set('tag', $('tagFilter').value.trim());
  if ($('statusFilter').value) query.set('status', $('statusFilter').value);
  query.set('sort', $('sortFilter').value || 'name');
  const [serviceData, dashboard, mailData] = await Promise.all([
    api('/services?' + query),
    api('/dashboard'),
    api('/domain/monitors').catch(() => ({ monitors: [] }))
  ]);
  services = serviceData.services || [];
  mailMonitors = mailData.monitors || [];
  const mailHistoryItems = await Promise.all(mailMonitors.map(async monitor => {
    try {
      const result = await api(`/domain/checks?domain=${encodeURIComponent(monitor.domain)}`);
      return [monitor.domain, result.checks || []];
    } catch (_) {
      return [monitor.domain, []];
    }
  }));
  mailHistory = Object.fromEntries(mailHistoryItems);
   const historyItems = await Promise.all(services.map(async service => {
     try {
       const result = await api(`/services/${service.id}/checks`);
       const checks = result.checks || [];
       if (!checks.length && service.last_check) checks.push(service.last_check);
       return [service.id, checks];
     } catch (_) {
       return [service.id, []];
     }
   }));
   serviceHistory = Object.fromEntries(historyItems);
  renderDashboard(dashboard); renderServiceSidebar(); renderMailSidebar(); renderServices();
}
async function checkService(id, button) { button.disabled = true; try { await api(`/services/${id}/check`, { method: 'POST' }); await load(); } catch (error) { alert('Ошибка проверки: ' + error.message); } finally { button.disabled = false; } }
async function checkAll() { $('checkAll').disabled = true; try { for (const service of services.filter(item => item.enabled)) await api(`/services/${service.id}/check`, { method: 'POST' }); await load(); } catch (error) { alert('Ошибка проверки: ' + error.message); } finally { $('checkAll').disabled = false; } }
function openNew() { editingId = null; $('modalTitle').textContent = 'Новый сервис'; $('serviceForm').reset(); $('checkType').value = 'http'; updateCheckTypeHint(); $('serviceCheckInterval').value = '30s'; $('repeatNotificationsEnabled').checked = false; $('repeatNotificationInterval').value = '5m'; $('enabled').checked = true; $('modal').classList.remove('hidden'); }
function openEdit(id) { const service = services.find(item => item.id === id); if (!service) return; editingId = id; $('modalTitle').textContent = 'Редактирование сервиса'; $('name').value = service.name; $('checkType').value = canonicalCheckType(service.check_type); updateCheckTypeHint(); $('url').value = service.url; $('environment').value = service.environment; $('serviceCheckInterval').value = service.check_interval || '30s'; $('repeatNotificationsEnabled').checked = service.repeat_notifications_enabled; $('repeatNotificationInterval').value = service.repeat_notification_interval || '5m'; $('description').value = service.description; $('tags').value = (service.tags || []).join(', '); $('enabled').checked = service.enabled; $('modal').classList.remove('hidden'); }
function closeModal() { $('modal').classList.add('hidden'); }
async function openDetail(id) {
  const data = await api(`/services/${id}`); const service = data.service;
  const latestTLSCheck = (data.checks || []).find(check => check.tls);
  $('detailTitle').textContent = service.name; $('detailContent').innerHTML = `<p>${escapeHtml(service.description || '')}</p><div id="serviceMetrics" class="service-metrics-loading">Загрузка статистики…</div><button id="clearHistory">Очистить историю старше 30 дней</button><h3>История проверок</h3><div class="compact-list">${(data.checks || []).map(c => `<div class="list-row"><span class="badge status-${escapeHtml(c.status)}">${escapeHtml(c.status)}</span><span>${c.http_status || '—'} · ${c.response_time_ms} ms ${escapeHtml(c.error || '')}${diagnosticsSummary(c)}</span><small>${new Date(c.checked_at).toLocaleString()}</small></div>`).join('') || '<span class="muted">Нет проверок</span>'}</div><h3>Runbooks</h3><div id="runbooks">${(data.runbooks || []).map(r => `<article class="runbook"><div class="runbook-head"><h4>${escapeHtml(r.title)}</h4><div><button data-runbook-edit="${r.id}">Изменить</button><button data-runbook-delete="${r.id}">Удалить</button></div></div><div>${r.rendered_html || ''}</div></article>`).join('') || '<span class="muted">Runbooks нет</span>'}</div><form id="runbookForm" class="inline-form"><input id="runbookId" type="hidden"><input id="runbookTitle" placeholder="Название инструкции" required><textarea id="runbookContent" placeholder="Markdown source" required></textarea><div class="form-actions"><button type="button" id="previewRunbook">Preview</button><button class="primary">Сохранить runbook</button></div><div id="runbookPreview" class="preview"></div></form>`;
  if (latestTLSCheck) $('detailContent').insertAdjacentHTML('afterbegin', `<section class="certificate-panel"><h3>Проверка TLS-сертификата</h3>${certificateSummary(latestTLSCheck)}<p class="muted">Последняя проверка: ${new Date(latestTLSCheck.checked_at).toLocaleString()}</p><button id="checkCertificate" type="button">Проверить сейчас</button></section>`);
  $('detailContent').insertAdjacentHTML('afterbegin', `<h3>График проверок</h3>${renderCheckChart(data.checks)}`);
  loadServiceMetrics(id);
  $('runbookForm').onsubmit = async event => { event.preventDefault(); const runbookId = $('runbookId').value; await api(runbookId ? `/runbooks/${runbookId}` : `/services/${id}/runbooks`, { method: runbookId ? 'PUT' : 'POST', body: JSON.stringify({ title: $('runbookTitle').value, content: $('runbookContent').value }) }); await openDetail(id); };
  $('previewRunbook').onclick = async () => { const preview = await api('/runbooks/preview', { method: 'POST', body: JSON.stringify({ title: $('runbookTitle').value, content: $('runbookContent').value }) }); $('runbookPreview').innerHTML = preview.html; };
  $('clearHistory').onclick = async () => { await api(`/services/${id}/checks/clear`, { method: 'POST' }); await openDetail(id); };
  if ($('checkCertificate')) $('checkCertificate').onclick = async () => { $('checkCertificate').disabled = true; try { await api(`/services/${id}/check`, { method: 'POST' }); await openDetail(id); await load(); } finally { $('checkCertificate').disabled = false; } };
  $('detailContent').querySelectorAll('[data-runbook-edit]').forEach(button => button.onclick = () => { const item = data.runbooks.find(runbook => runbook.id === Number(button.dataset.runbookEdit)); if (!item) return; $('runbookId').value = item.id; $('runbookTitle').value = item.title; $('runbookContent').value = item.content; $('runbookPreview').innerHTML = item.rendered_html || ''; });
  $('detailContent').querySelectorAll('[data-runbook-delete]').forEach(button => button.onclick = async () => { if (!confirm('Удалить runbook?')) return; await api(`/runbooks/${button.dataset.runbookDelete}`, { method: 'DELETE' }); await openDetail(id); });
  $('detailModal').classList.remove('hidden');
}
async function deleteService(id) { if (!confirm('Удалить сервис и его историю проверок?')) return; try { await api(`/services/${id}`, { method: 'DELETE' }); await load(); } catch (error) { alert('Ошибка удаления: ' + error.message); } }
async function showNotifications() { const data = await api('/notifications'); if ('Notification' in window && Notification.permission === 'default') await Notification.requestPermission(); for (const item of data.notifications.filter(n => !n.read)) { if ('Notification' in window && Notification.permission === 'granted') new Notification(item.title, { body: item.message }); } await api('/notifications/read', { method: 'POST' }); alert(data.notifications.length ? data.notifications.map(n => `${n.title}: ${n.message}`).join('\n') : 'Новых уведомлений нет'); }

$('newService').onclick = openNew; $('closeModal').onclick = closeModal; $('cancel').onclick = closeModal; $('checkAll').onclick = checkAll; $('applyFilters').onclick = load; $('notificationsButton').onclick = showNotifications; $('closeDetail').onclick = () => $('detailModal').classList.add('hidden');
$('cardView').onclick = () => { viewMode = 'cards'; localStorage.setItem('uptime-view-mode-v2', viewMode); applyViewMode(); renderServices(); };
$('tableView').onclick = () => { viewMode = 'table'; localStorage.setItem('uptime-view-mode-v2', viewMode); applyViewMode(); renderServices(); };
$('serviceSearch').onkeydown = event => { if (event.key === 'Enter') load(); };
applyViewMode();
 $('serviceForm').onsubmit = async event => { event.preventDefault(); const payload = { name: $('name').value.trim(), url: $('url').value.trim(), check_type: canonicalCheckType($('checkType').value), environment: $('environment').value, check_interval: $('serviceCheckInterval').value.trim(), repeat_notifications_enabled: $('repeatNotificationsEnabled').checked, repeat_notification_interval: $('repeatNotificationInterval').value.trim(), description: $('description').value.trim(), tags: $('tags').value.split(',').map(value => value.trim()).filter(Boolean), enabled: $('enabled').checked }; try { await api(editingId ? `/services/${editingId}` : '/services', { method: editingId ? 'PUT' : 'POST', body: JSON.stringify(payload) }); closeModal(); await load(); } catch (error) { alert('Ошибка сохранения: ' + error.message); } };
load().catch(error => { $('serviceList').innerHTML = `<div class="empty">Не удалось загрузить dashboard: ${escapeHtml(error.message)}</div>`; });
