const mailAPI = '/api/v1';
const mail$ = id => document.getElementById(id);
const mailEscape = value => String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
async function mailApi(path, options = {}) {
  const response = await fetch(mailAPI + path, { headers: { 'Content-Type': 'application/json' }, ...options });
  if (!response.ok) {
    const body = await response.text();
    try {
      const parsed = JSON.parse(body);
      throw new Error(parsed.error || body || response.statusText);
    } catch (error) {
      if (error instanceof SyntaxError) throw new Error(body || response.statusText);
      throw error;
    }
  }
  if (response.status === 204) return null;
  const body = await response.text();
  try {
    return JSON.parse(body);
  } catch (error) {
    throw new Error(`Сервер вернул не JSON (HTTP ${response.status}): ${body.slice(0, 180) || 'пустой ответ'}`);
  }
}
function mailStatus(status) { return `<span class="mail-status mail-status-${mailEscape(status)}">${mailEscape(status)}</span>`; }
function domainCheckIcon(name, section) {
  const status = section?.status || 'pending';
  const reason = section ? mailScoreReason(section) : 'Проверка ещё не выполнялась';
  return `<span class="domain-check-item domain-check-${mailEscape(status)}" title="${mailEscape(`${name}: ${reason}`)}" aria-label="${mailEscape(`${name}: ${reason}`)}"><i class="domain-check-dot"></i><strong>${name}</strong></span>`;
}
function domainCheckIndicators(check) {
  const result = check?.result;
  return `<div class="domain-check-indicators">${domainCheckIcon('MX', result?.mx)}${domainCheckIcon('SPF', result?.spf)}${domainCheckIcon('DKIM', result?.dkim)}${domainCheckIcon('DMARC', result?.dmarc)}${domainCheckIcon('DNSBL/RBL', result?.blacklists)}${domainCheckIcon('WEB', result?.website)}</div>`;
}
function renderDomainMonitorCard(item) {
  const check = item.last_check;
  return `<article class="domain-card" data-domain-card-id="${item.id}"><div class="domain-card-main"><div><h2>${mailEscape(item.domain)}</h2><p class="muted">Проверка каждые ${mailEscape(item.check_interval)} · ${item.last_checked ? `последняя: ${new Date(item.last_checked).toLocaleString()}` : 'первая проверка скоро'}</p>${domainCheckIndicators(check)}</div><div class="domain-card-status">${item.enabled ? mailStatus(item.last_status || 'ожидает') : mailStatus('выключен')}<strong>${item.last_checked ? `${mailEscape(item.last_score)}/100` : '—'}</strong></div></div><div class="domain-card-actions"><a class="primary domain-open-link" href="/uptime/domain/${item.id}">Открыть диагностику</a><button type="button" data-monitor-edit="${item.id}">Изменить</button><button type="button" data-monitor-toggle="${item.id}" data-enabled="${item.enabled}">${item.enabled ? 'Выключить' : 'Включить'}</button><button type="button" data-monitor-delete="${item.id}">Удалить</button></div></article>`;
}
function mailCard(title, data, details) { const cardClass = title === 'DNSBL' ? ' mail-card-dnsbl' : ''; return `<article class="mail-card${cardClass}"><h3>${mailEscape(title)}</h3>${mailStatus(data.status)}${details}</article>`; }
function generateDmarcRecord() {
  const domain = mail$('dmarcDomain').value.trim().toLowerCase().replace(/\.$/, '');
  const policy = mail$('dmarcPolicy').value;
  const rua = mail$('dmarcRua').value.trim();
  const ruf = mail$('dmarcRuf').value.trim();
  const pct = Math.max(0, Math.min(100, Number(mail$('dmarcPct').value || 100)));
  const parts = [`v=DMARC1`, `p=${policy}`];
  if (rua) parts.push(`rua=${rua}`);
  if (ruf) parts.push(`ruf=${ruf}`);
  if (pct !== 100) parts.push(`pct=${pct}`);
  if (mail$('dmarcAdkim').value !== 'r') parts.push(`adkim=${mail$('dmarcAdkim').value}`);
  if (mail$('dmarcAspf').value !== 'r') parts.push(`aspf=${mail$('dmarcAspf').value}`);
  if (mail$('dmarcFo').value !== '0') parts.push(`fo=${mail$('dmarcFo').value}`);
  mail$('dmarcRecord').textContent = parts.join('; ');
  mail$('dmarcMessage').textContent = domain ? `Добавьте TXT-запись для _dmarc.${domain}` : 'Укажите домен, чтобы получить имя записи.';
}
function loadDmarcIntoGenerator(record, domain, shouldScroll = true) {
  const values = {};
  String(record || '').split(';').forEach(part => { const separator = part.indexOf('='); if (separator > 0) values[part.slice(0, separator).trim().toLowerCase()] = part.slice(separator + 1).trim(); });
  mail$('dmarcDomain').value = domain || mail$('dmarcDomain').value;
  if (['none', 'quarantine', 'reject'].includes(values.p)) mail$('dmarcPolicy').value = values.p;
  ['rua', 'ruf'].forEach(key => { mail$(`dmarc${key[0].toUpperCase()}${key.slice(1)}`).value = values[key] || ''; });
  if (/^\d+$/.test(values.pct || '')) mail$('dmarcPct').value = Math.max(0, Math.min(100, Number(values.pct)));
  if (values.adkim === 's' || values.adkim === 'r') mail$('dmarcAdkim').value = values.adkim;
  if (values.aspf === 's' || values.aspf === 'r') mail$('dmarcAspf').value = values.aspf;
  if (['0', '1', 'd', 's'].includes(values.fo)) mail$('dmarcFo').value = values.fo;
  generateDmarcRecord();
  if (shouldScroll) mail$('dmarcGenerator').scrollIntoView({ behavior: 'smooth', block: 'start' });
}
mail$('dmarcGeneratorForm').onsubmit = event => { event.preventDefault(); generateDmarcRecord(); };
['dmarcDomain', 'dmarcPolicy', 'dmarcRua', 'dmarcRuf', 'dmarcPct', 'dmarcAdkim', 'dmarcAspf', 'dmarcFo'].forEach(id => { mail$(id).addEventListener('input', generateDmarcRecord); mail$(id).addEventListener('change', generateDmarcRecord); });
mail$('copyDmarc').onclick = async () => { generateDmarcRecord(); try { await navigator.clipboard.writeText(mail$('dmarcRecord').textContent); mail$('dmarcMessage').textContent = 'DMARC-запись скопирована в буфер обмена.'; } catch (error) { mail$('dmarcMessage').textContent = 'Не удалось скопировать автоматически. Выделите запись вручную.'; } };
function mailScorePenalty(status) {
  return { warning: -15, not_found: -20, error: -30 }[status] || 0;
}
function mailScoreReason(section) {
  if (section.status === 'ok') return 'Нарушений не выявлено';
  if (section.different) return `Различия: ${(section.differences || []).join(', ')}`;
  if (section.listed > 0) return `Найдено в DNSBL/RBL: ${section.listed}`;
  if (section.message) return section.message;
  if (section.status === 'not_found') return 'Запись или результат не найдены';
  if (section.status === 'error') return 'Проверка завершилась ошибкой';
  return 'Проверка выявила предупреждение';
}
function renderMailScoreBreakdown(result, score) {
  const sections = [['MX', result.mx], ['SPF', result.spf], ['DKIM', result.dkim], ['DMARC', result.dmarc], ['DNSBL/RBL', result.blacklists], ['Сайт', result.website]];
  const rows = sections.map(([name, section]) => {
    const penalty = mailScorePenalty(section.status);
    const penaltyText = penalty === 0 ? '0' : `−${Math.abs(penalty)}`;
    const triggered = penalty < 0 ? '<span class="score-triggered">Сработало</span>' : '<span class="score-clear">Нет</span>';
    return `<div class="score-row"><strong>${name}</strong><span>${mailStatus(section.status)}</span><span>${triggered} <small>${mailEscape(mailScoreReason(section))}</small></span><span class="score-penalty ${penalty < 0 ? 'score-penalty-negative' : ''}">${penaltyText}</span></div>`;
  }).join('');
  const triggered = sections.filter(([, section]) => mailScorePenalty(section.status) < 0).map(([name, section]) => `${name} (${section.status})`).join(', ');
  return `<section class="mail-score-breakdown"><h3>Расчёт Mail Health Score</h3><p class="muted">Старт: 100 баллов. Штраф применяется отдельно за каждый статус.</p><p class="score-trigger-summary"><strong>Сработали:</strong> ${triggered ? mailEscape(triggered) : 'нет'}</p><div class="score-table"><div class="score-row score-head"><strong>Проверка</strong><strong>Статус</strong><strong>Срабатывание</strong><strong>Изменение</strong></div>${rows}</div><div class="score-total"><span>Итого</span><strong>${score}/100</strong></div><p class="muted">warning −15 · not_found −20 · error −30 · ok 0</p></section>`;
}
function renderMailResult(check) {
  const result = check.result;
  const mxDetails = (result.mx.records || []).map(item => `<p>MX ${item.priority}: ${mailEscape(item.host)}${item.addresses?.length ? ` · ${item.addresses.join(', ')}` : ''}</p>`).join('') || `<p>${mailEscape(result.mx.message || 'Нет записей')}</p>`;
  const spfDetails = `<p>${mailEscape(result.spf.policy || 'policy не указана')}</p><p>DNS lookup: ${result.spf.dns_lookups}</p><p>${mailEscape(result.spf.message || '')}</p>`;
  const dkimDetails = `<p>Selector: ${mailEscape(result.dkim.selector || check.dkim_selector || '—')}</p><p>${mailEscape(result.dkim.message || '')}</p>`;
  const dmarcGeneratorButton = result.dmarc.record ? '<button type="button" id="useCurrentDmarc">Расширить текущую запись</button>' : '';
  const dmarcDetails = `<p>Policy: ${mailEscape(result.dmarc.policy || 'не указана')}</p>${result.dmarc.record ? `<p><strong>Текущая TXT-запись:</strong><br><code>${mailEscape(result.dmarc.record)}</code></p>` : ''}<p>${mailEscape(result.dmarc.rua || '')}</p><p>${mailEscape(result.dmarc.message || '')}</p>${dmarcGeneratorButton}`;
  const websiteDetails = (result.website.results || []).map(item => `<p class="mail-alert-detail ${item.status === 'ok' ? 'mail-detail-ok' : 'mail-detail-error'}"><strong>${mailEscape(item.url)}</strong>${item.final_url && item.final_url !== item.url ? ` → ${mailEscape(item.final_url)}` : ''} · ${item.http_status || 'error'}${item.content_length ? ` · ${item.content_length} bytes` : ''}${item.error ? ` · ${mailEscape(item.error)}` : ''}</p>`).join('') + (result.website.different ? `<p class="mail-alert-detail mail-detail-warning"><strong>Различия:</strong> ${mailEscape((result.website.differences || []).join(', '))}</p>` : '') || '<p>Проверка сайта ещё не выполнялась.</p>';
  const renderBlacklistRows = (results, problemsOnly = false) => (results || []).filter(item => !problemsOnly || item.status !== 'ok').map(item => {
    const status = item.status || (item.listed ? 'listed' : item.error ? 'timeout' : 'ok');
    const statusLabel = status === 'listed' ? 'LISTED' : status === 'timeout' ? 'TIMEOUT' : 'OK';
    const reason = item.reason || item.error || (item.listed ? 'IP is listed' : 'Not listed');
    const details = item.info_url ? ` <a href="${mailEscape(item.info_url)}" target="_blank" rel="noreferrer">Detail</a>` : '';
    const delist = item.delist_url ? ` <a href="${mailEscape(item.delist_url)}" target="_blank" rel="noreferrer">Delist</a>` : '';
    const zone = item.zone ? ` · ${mailEscape(item.zone)}` : '';
    return `<tr><td><span class="blacklist-result blacklist-result-${mailEscape(status)}"><i></i>${statusLabel}</span></td><td><strong>${mailEscape(item.list || item.zone)}</strong><small>${mailEscape(item.ip)}${zone}</small></td><td>${mailEscape(reason)}${details}${delist}</td><td>${item.ttl || '—'}</td><td>${item.response_time_ms || 0} ms</td></tr>`;
  }).join('');
  const blacklistRows = renderBlacklistRows(result.blacklists.results);
  const blacklistProblemRows = renderBlacklistRows(result.blacklists.results, true);
  const providerName = result.blacklists.provider === 'mxtoolbox' ? 'MxToolbox API' : result.blacklists.provider === 'local_dnsbl' ? 'Local DNSBL' : result.blacklists.provider;
  const provider = providerName ? `<span>Источник: <strong>${mailEscape(providerName)}</strong></span>` : '';
  const providerError = result.blacklists.provider_error ? `<p class="mail-alert-detail">Ошибка MxToolbox: ${mailEscape(result.blacklists.provider_error)}</p>` : '';
  const additionalError = result.blacklists.additional_error ? `<p class="mail-alert-detail">Дополнительная проверка MxToolbox: ${mailEscape(result.blacklists.additional_error)}</p>` : '';
  const tableHead = '<thead><tr><th>Статус</th><th>Blacklist</th><th>Reason</th><th>TTL</th><th>ResponseTime</th></tr></thead>';
  const localProblems = `<div class="blacklist-problems"><strong>Проблемные результаты: ${result.blacklists.listed + (result.blacklists.timeout || 0)}</strong>${blacklistProblemRows ? `<div class="blacklist-table-wrap"><table class="blacklist-table">${tableHead}<tbody>${blacklistProblemRows}</tbody></table></div>` : '<p class="muted">Проблем не найдено.</p>'}</div>`;
  const localTable = `${localProblems}<details class="blacklist-details"><summary>Показать полный список локальных результатов (${result.blacklists.checked})</summary><div class="blacklist-table-wrap"><table class="blacklist-table">${tableHead}<tbody>${blacklistRows || '<tr><td colspan="5">Нет результатов DNSBL-проверки</td></tr>'}</tbody></table></div></details>`;
  const additionalChecked = result.blacklists.additional_checked || 0;
  const additionalListed = result.blacklists.additional_listed || 0;
  const additionalTimeout = result.blacklists.additional_timeout || 0;
  const additionalRows = result.blacklists.additional_results || [];
  const additionalProblemRows = renderBlacklistRows(additionalRows, true);
  const additionalTable = result.blacklists.additional_provider ? `<div class="blacklist-additional"><div class="blacklist-additional-title"><strong>Дополнительная проверка: MxToolbox API</strong><span>Проверено: ${additionalChecked} · Найдено: ${additionalListed} · Timeout: ${additionalTimeout}</span></div>${additionalError}${additionalProblemRows ? `<div class="blacklist-problems"><strong>Проблемные результаты: ${additionalListed + additionalTimeout}</strong><div class="blacklist-table-wrap"><table class="blacklist-table">${tableHead}<tbody>${additionalProblemRows}</tbody></table></div></div>` : ''}<details class="blacklist-details"><summary>Показать полный список MxToolbox (${additionalChecked})</summary><div class="blacklist-table-wrap"><table class="blacklist-table">${tableHead}<tbody>${renderBlacklistRows(additionalRows) || '<tr><td colspan="5">Нет результатов MxToolbox</td></tr>'}</tbody></table></div></details></div>` : '';
  const blacklistDetails = `<div class="blacklist-summary">${provider}<span>Проверено: <strong>${result.blacklists.checked}</strong></span><span>Найдено: <strong>${result.blacklists.listed}</strong></span><span>Timeout: <strong>${result.blacklists.timeout || 0}</strong></span></div>${providerError}${localTable}${additionalTable}${mailEscape(result.blacklists.message || '')}`;
  const registrationKnown = result.registration.status || result.registration.expires_at || result.registration.registrar;
  const registrationChanges = (result.registration.changes || []).map(change => `<div class="registration-change"><strong>${mailEscape(change.field)}</strong><span><b>Было:</b> ${mailEscape(change.previous || '—')}</span><span><b>Стало:</b> ${mailEscape(change.current || '—')}</span></div>`).join('');
  const registrationMessage = result.registration.message || (!registrationKnown ? 'Результат создан до включения RDAP. Запустите новую проверку.' : (!result.registration.changed ? 'Изменений относительно предыдущей проверки нет.' : ''));
  const noRegistrationChanges = !result.registration.changed && registrationKnown && !result.registration.message;
  const registrationDetails = `${result.registration.changed ? `<div class="registration-changes"><strong>Что изменилось с прошлой проверки</strong>${registrationChanges}</div>` : ''}<p>Регистратор: ${mailEscape(result.registration.registrar || 'не указан')}</p><p>Окончание регистрации: ${result.registration.expires_at ? new Date(result.registration.expires_at).toLocaleString() : 'не определено'}</p><p>Осталось: ${result.registration.expires_at ? `${result.registration.days_remaining} дн.` : '—'}</p>${registrationMessage ? `<p class="${noRegistrationChanges ? 'registration-no-changes' : ''}">${mailEscape(registrationMessage)}</p>` : ''}`;
  const mxTLSDetails = (result.mx_tls.results || []).map(item => { const detailClass = item.status === 'ok' ? 'mail-detail-ok' : item.status === 'expiring' ? 'mail-detail-warning' : 'mail-detail-error'; return `<p class="mail-alert-detail ${detailClass}"><strong>${mailEscape(item.host)}:${mailEscape(item.port)}</strong>${item.address ? ` <small>(${mailEscape(item.address)})</small>` : ''} · ${mailEscape(item.tls_mode || 'TLS')} · ${mailEscape(item.status)}${item.expires_at ? ` · до ${new Date(item.expires_at).toLocaleDateString()}` : ''}${item.error ? ` · ${mailEscape(item.error)}` : ''}</p>`; }).join('') || '<p>Нет MX для TLS-проверки</p>';
  const savedAI = check.ai_report ? `<div id="aiReport" class="ai-report">${renderAIReportMarkdown(check.ai_report)}<small class="ai-report-meta">Сохранено: ${check.ai_updated_at ? new Date(check.ai_updated_at).toLocaleString() : '—'} · ${mailEscape(check.ai_model || 'AI')}</small></div>` : check.ai_error ? `<div id="aiReport" class="ai-report ai-report-error"><strong>Последняя ошибка AI:</strong><p>${mailEscape(check.ai_error)}</p><small class="ai-report-meta">Сохранено: ${check.ai_updated_at ? new Date(check.ai_updated_at).toLocaleString() : '—'} · ${mailEscape(check.ai_model || 'AI')}</small></div>` : '<div id="aiReport" class="ai-report hidden"></div>';
  const addMonitorButton = isDomainDetailPage() ? '' : '<button type="button" id="addResultMonitor">Добавить в постоянный мониторинг</button>';
  mail$('mailResult').innerHTML = `<article class="mail-card mail-summary"><h2>${mailEscape(check.domain)}</h2><div class="mail-score">${check.score}/100</div>${mailStatus(check.status)}<p>Проверено: ${new Date(check.checked_at).toLocaleString()}</p><button type="button" id="refreshResultCheck">Проверить заново</button><button type="button" id="generateAIReport">Сформировать AI-отчёт</button>${addMonitorButton}${savedAI}</article>${renderMailScoreBreakdown(result, check.score)}${mailCard('MX', result.mx, mxDetails)}${mailCard('MX TLS', result.mx_tls, mxTLSDetails)}${mailCard('Сайт', result.website, websiteDetails)}${mailCard('SPF', result.spf, spfDetails)}${mailCard('DKIM', result.dkim, dkimDetails)}${mailCard('DMARC', result.dmarc, dmarcDetails)}${mailCard('DNSBL', result.blacklists, blacklistDetails)}${mailCard('Регистрация', result.registration, registrationDetails)}`;
  mail$('mailResult').classList.remove('hidden');
  if (result.dmarc.record && mail$('useCurrentDmarc')) mail$('useCurrentDmarc').onclick = () => loadDmarcIntoGenerator(result.dmarc.record, check.domain);
  if (result.dmarc.record) loadDmarcIntoGenerator(result.dmarc.record, check.domain, false);
  mail$('refreshResultCheck').onclick = async () => {
    mail$('refreshResultCheck').disabled = true;
    try {
      const fresh = await mailApi('/domain/check', { method: 'POST', body: JSON.stringify({ domain: check.domain, dkim_selector: check.dkim_selector || result.dkim.selector || '' }) });
      renderMailResult(fresh); await loadMailHistory(fresh.domain);
    } catch (error) { mail$('mailMessage').textContent = 'Ошибка повторной проверки: ' + error.message; } finally { if (mail$('refreshResultCheck')) mail$('refreshResultCheck').disabled = false; }
  };
  mail$('generateAIReport').onclick = async () => {
    const button = mail$('generateAIReport');
    const report = mail$('aiReport');
    button.disabled = true;
    report.classList.remove('hidden');
    report.textContent = 'AI анализирует результат проверки…';
    try {
      const data = await mailApi(`/domain/checks/${check.id}/ai-report`, { method: 'POST' });
      check.ai_report = data.report;
      check.ai_error = '';
      check.ai_model = data.model || '';
      check.ai_updated_at = data.generated_at;
      report.innerHTML = renderAIReportMarkdown(data.report);
    } catch (error) {
      report.textContent = 'Ошибка AI-отчёта: ' + friendlyAIError(error.message);
    } finally { button.disabled = false; }
  };
  if (mail$('addResultMonitor')) mail$('addResultMonitor').onclick = () => { mail$('monitorDomain').value = check.domain; mail$('monitorSelector').value = result.dkim.selector || check.dkim_selector || ''; mail$('monitorDomain').focus(); mail$('monitorMessage').textContent = 'Поля мониторинга заполнены. Нажмите «Добавить мониторинг».'; mail$('monitorForm').scrollIntoView({ behavior: 'smooth', block: 'center' }); };
}
async function loadMailHistory(domain) {
  const data = await mailApi(`/domain/checks?domain=${encodeURIComponent(domain)}`);
  mail$('mailHistory').innerHTML = (data.checks || []).map(item => `<div class="history-mail-row"><span>${mailStatus(item.status)} <strong>${mailEscape(item.score)}/100</strong></span><span class="history-mail-actions"><small>${new Date(item.checked_at).toLocaleString()}</small><button type="button" data-mail-detail="${item.id}">Детали</button></span></div>`).join('') || '<div class="muted">История пуста.</div>';
  bindMailDetailButtons();
  return data.checks || [];
}
async function showMailCheck(id) {
  try {
    const check = await mailApi(`/domain/checks/${id}`);
    renderMailResult(check);
    mail$('mailResult').scrollIntoView({ behavior: 'smooth', block: 'start' });
  } catch (error) {
    mail$('mailMessage').textContent = 'Ошибка загрузки деталей: ' + error.message;
  }
}
function bindMailDetailButtons() {
  document.querySelectorAll('[data-mail-detail]').forEach(button => {
    button.onclick = () => showMailCheck(button.dataset.mailDetail);
  });
}
async function loadMailMonitors() {
  const data = await mailApi('/domain/monitors');
  const monitors = await Promise.all((data.monitors || []).map(async item => {
    try {
      const history = await mailApi(`/domain/checks?domain=${encodeURIComponent(item.domain)}`);
      return { ...item, last_check: history.checks?.[0] || null };
    } catch (_) {
      return { ...item, last_check: null };
    }
  }));
  mail$('mailMonitors').innerHTML = monitors.map(renderDomainMonitorCard).join('') || '<div class="muted">Домены для мониторинга ещё не добавлены.</div>';
  mail$('domainCards').innerHTML = monitors.map(renderDomainMonitorCard).join('') || '<div class="muted">Домены для мониторинга ещё не добавлены.</div>';
  mail$('domainCards').querySelectorAll('[data-domain-card-id]').forEach(card => card.onclick = event => { if (event.target.closest('a, button, input, select, textarea')) return; window.location.href = `/uptime/domain/${card.dataset.domainCardId}`; });
  document.querySelectorAll('[data-monitor-toggle]').forEach(button => button.onclick = async () => { await mailApi(`/domain/monitors/${button.dataset.monitorToggle}`, { method: 'PUT', body: JSON.stringify({ enabled: button.dataset.enabled !== 'true' }) }); await loadMailMonitors(); });
  document.querySelectorAll('[data-monitor-edit]').forEach(button => button.onclick = () => {
     const monitor = monitors.find(item => item.id === Number(button.dataset.monitorEdit));
    if (!monitor) return;
    mail$('monitorEditId').value = monitor.id;
    mail$('monitorDomain').value = monitor.domain;
    mail$('monitorSelector').value = monitor.dkim_selector || '';
    mail$('monitorInterval').value = monitor.check_interval || '1h';
    mail$('monitorSubmit').textContent = 'Сохранить изменения';
    mail$('monitorCancel').classList.remove('hidden');
    mail$('monitorMessage').textContent = 'Редактирование мониторинга';
    mail$('monitorForm').scrollIntoView({ behavior: 'smooth', block: 'center' });
  });
  document.querySelectorAll('[data-monitor-delete]').forEach(button => button.onclick = async () => { if (confirm('Удалить постоянный мониторинг?')) { await mailApi(`/domain/monitors/${button.dataset.monitorDelete}`, { method: 'DELETE' }); await loadMailMonitors(); } });
  return monitors;
}
mail$('mailForm').onsubmit = async event => {
  event.preventDefault();
  const domain = mail$('mailDomain').value.trim();
  mail$('mailMessage').textContent = 'Выполняется DNS-проверка…';
  try {
    const result = await mailApi('/domain/check', { method: 'POST', body: JSON.stringify({ domain, dkim_selector: mail$('dkimSelector').value.trim() }) });
    renderMailResult(result);
    await loadMailHistory(result.domain);
    mail$('mailMessage').textContent = 'Проверка завершена';
  } catch (error) {
    mail$('mailMessage').textContent = 'Ошибка: ' + error.message;
  }
};
function friendlyAIError(message) {
  const text = String(message || 'неизвестная ошибка');
  const lower = text.toLowerCase();
  if (lower.includes('<!doctype html') || lower.includes('<html')) {
    const code = text.match(/(?:error code|status code)\s*(\d{3})/i)?.[1] || '502';
    return `AI endpoint вернул HTML-страницу Cloudflare (HTTP ${code}). Проверьте, что production запущен на последней версии и endpoint AI доступен с сервера.`;
  }
  return text;
}
function safeMarkdownHref(value) {
  const href = String(value || '').trim();
  if (/^(https?:\/\/|mailto:)/i.test(href)) return href;
  return '#';
}
function renderAIInlineMarkdown(value) {
  let html = mailEscape(value);
  html = html.replace(/`([^`]+)`/g, '<code>$1</code>');
  html = html.replace(/\[([^\]]+)\]\(([^)]+)\)/g, (_, label, href) => `<a href="${mailEscape(safeMarkdownHref(href))}" target="_blank" rel="noopener noreferrer">${label}</a>`);
  html = html.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  html = html.replace(/__([^_]+)__/g, '<strong>$1</strong>');
  html = html.replace(/(^|[^*])\*([^*]+)\*/g, '$1<em>$2</em>');
  html = html.replace(/(^|[^_])_([^_]+)_/g, '$1<em>$2</em>');
  return html;
}
function renderAIReportMarkdown(markdown) {
  const lines = String(markdown || '').replace(/\r\n?/g, '\n').split('\n');
  const output = [];
  let inCode = false;
  let listType = '';
  const closeList = () => { if (listType) { output.push(`</${listType}>`); listType = ''; } };
  const tableCells = line => line.trim().replace(/^\|/, '').replace(/\|$/, '').split('|').map(cell => cell.trim());
  const tableSeparator = line => tableCells(line).length > 0 && tableCells(line).every(cell => /^:?-{3,}:?$/.test(cell));
  for (let index = 0; index < lines.length; index += 1) {
    const line = lines[index];
    if (/^\s*```/.test(line)) {
      if (inCode) { output.push('</code></pre>'); inCode = false; }
      else { closeList(); output.push('<pre><code>'); inCode = true; }
      continue;
    }
    if (inCode) { output.push(mailEscape(line) + '\n'); continue; }
    if (!line.trim()) { closeList(); continue; }
    if (line.trim().startsWith('|') && index + 1 < lines.length && tableSeparator(lines[index + 1])) {
      closeList();
      const header = tableCells(line);
      const rows = [];
      index += 2;
      while (index < lines.length && lines[index].trim().startsWith('|')) {
        rows.push(tableCells(lines[index]));
        index += 1;
      }
      index -= 1;
      output.push(`<div class="ai-table-wrap"><table class="ai-table"><thead><tr>${header.map(cell => `<th>${renderAIInlineMarkdown(cell)}</th>`).join('')}</tr></thead><tbody>${rows.map(row => `<tr>${header.map((_, cellIndex) => `<td>${renderAIInlineMarkdown(row[cellIndex] || '')}</td>`).join('')}</tr>`).join('')}</tbody></table></div>`);
      continue;
    }
    const heading = line.match(/^\s*(#{1,6})\s+(.+)$/);
    if (heading) { closeList(); const level = heading[1].length; output.push(`<h${level}>${renderAIInlineMarkdown(heading[2])}</h${level}>`); continue; }
    if (/^\s*([-*_])(?:\s*\1){2,}\s*$/.test(line)) { closeList(); output.push('<hr>'); continue; }
    const unordered = line.match(/^\s*[-*+]\s+(.+)$/);
    const ordered = line.match(/^\s*\d+[.)]\s+(.+)$/);
    if (unordered || ordered) {
      const nextType = unordered ? 'ul' : 'ol';
      if (listType !== nextType) { closeList(); listType = nextType; output.push(`<${listType}>`); }
      output.push(`<li>${renderAIInlineMarkdown((unordered || ordered)[1])}</li>`);
      continue;
    }
    const quote = line.match(/^\s*>\s?(.*)$/);
    if (quote) { closeList(); output.push(`<blockquote>${renderAIInlineMarkdown(quote[1])}</blockquote>`); continue; }
    closeList();
    output.push(`<p>${renderAIInlineMarkdown(line)}</p>`);
  }
  if (inCode) output.push('</code></pre>');
  closeList();
  return output.join('');
}
mail$('monitorForm').onsubmit = async event => {
  event.preventDefault();
  mail$('monitorMessage').textContent = 'Сохранение…';
  try {
    const id = mail$('monitorEditId').value;
    const path = id ? `/domain/monitors/${id}` : '/domain/monitors';
    const method = id ? 'PUT' : 'POST';
    await mailApi(path, { method, body: JSON.stringify({ domain: mail$('monitorDomain').value.trim(), dkim_selector: mail$('monitorSelector').value.trim(), check_interval: mail$('monitorInterval').value.trim() }) });
    mail$('monitorMessage').textContent = id ? 'Изменения сохранены.' : 'Мониторинг добавлен. Первая проверка выполняется в фоне.';
    event.target.reset(); mail$('monitorEditId').value = ''; mail$('monitorInterval').value = '1h'; mail$('monitorSubmit').textContent = 'Добавить мониторинг'; mail$('monitorCancel').classList.add('hidden'); await loadMailMonitors();
  } catch (error) { mail$('monitorMessage').textContent = 'Ошибка: ' + error.message; }
};
mail$('monitorCancel').onclick = () => { mail$('monitorForm').reset(); mail$('monitorEditId').value = ''; mail$('monitorInterval').value = '1h'; mail$('monitorSubmit').textContent = 'Добавить мониторинг'; mail$('monitorCancel').classList.add('hidden'); mail$('monitorMessage').textContent = ''; };
function isDomainDetailPage() { return /^\/uptime\/domain\/\d+$/.test(window.location.pathname); }
function domainMonitorID() { return Number(window.location.pathname.split('/').pop()); }
function setupDomainPage(detail) {
  const checkPanel = mail$('domainCheckPanel');
  const historyPanel = mail$('domainHistoryPanel');
  const listPanel = mail$('domainListPanel');
  mail$('mailMonitors').classList.add('hidden');
  if (detail) {
    listPanel.classList.add('hidden');
    checkPanel.classList.remove('hidden');
    historyPanel.classList.remove('hidden');
  } else {
    listPanel.classList.remove('hidden');
    checkPanel.classList.add('hidden');
    historyPanel.classList.add('hidden');
    mail$('mailResult').classList.add('hidden');
  }
}
async function initializeDomainPage() {
  const detail = isDomainDetailPage();
  setupDomainPage(detail);
  const monitors = await loadMailMonitors();
  if (!detail) return;
  const monitor = monitors.find(item => item.id === domainMonitorID());
  if (!monitor) {
    mail$('mailMessage').textContent = 'Домен мониторинга не найден.';
    return;
  }
   mail$('domainDetailTitle').textContent = monitor.domain;
   mail$('mailDomain').value = monitor.domain;
   mail$('dmarcDomain').value = monitor.domain;
  mail$('dkimSelector').value = monitor.dkim_selector || '';
  const checks = await loadMailHistory(monitor.domain);
  if (checks.length) {
    const latest = checks[0];
    const lastAI = checks.find(item => item.ai_report || item.ai_error);
    if (lastAI && !latest.ai_report && !latest.ai_error) {
      latest.ai_report = lastAI.ai_report;
      latest.ai_error = lastAI.ai_error;
      latest.ai_model = lastAI.ai_model;
      latest.ai_updated_at = lastAI.ai_updated_at;
    }
    await showMailCheck(latest.id);
  }
}
initializeDomainPage().catch(error => { mail$('monitorMessage').textContent = 'Ошибка загрузки: ' + error.message; });
