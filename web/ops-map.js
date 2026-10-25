/* global L, api, escapeHtml, openDetail, statusLabel */
const OpsMap = (() => {
  let map;
  let markers;
  let timer;
  let request;
  let generation = 0;
  let signature = '';
  let bounds = [];
  let activeQuery = '';
  let tileErrors = 0;
  const element = id => document.getElementById(id);
  const colors = { online: '#35d07f', warning: '#f0b84b', offline: '#ef5367', unknown: '#737b88' };
  const node = (tag, text, className) => {
    const result = document.createElement(tag);
    if (text !== undefined) result.textContent = text;
    if (className) result.className = className;
    return result;
  };
  const date = value => value ? new Date(value).toLocaleString() : 'ещё не выполнялось';
  function tone(service) {
    if (!service.enabled || !service.last_check) return 'unknown';
    if (service.last_check.status === 'online') return 'online';
    if (service.last_check.status === 'offline') return 'offline';
    return 'warning';
  }
  function label(service) {
    return `${statusLabel(service.last_check)[1]}${service.enabled ? '' : ' · мониторинг выключен'}`;
  }
  function detailButton(service) {
    const button = node('button', 'Подробнее');
    button.type = 'button';
    button.setAttribute('aria-label', `Подробнее: ${service.name}`);
    button.onclick = () => openDetail(service.id).catch(error => window.alert(error.message));
    return button;
  }
  function serviceInfo(service, address) {
    const card = node('article', undefined, 'geo-service-info');
    const heading = node('div', undefined, 'geo-service-heading');
    const badge = node('span', label(service), `geo-status geo-${tone(service)}`);
    heading.append(node('strong', service.name), badge);
    card.append(heading, node('div', `${service.hostname}${address ? ` · ${address.ip}` : ''}`));
    if (address?.location?.last_success_at) {
      const location = address.location;
      card.append(node('p', [location.country, location.regionName, location.city].filter(Boolean).join(', ')));
      card.append(node('div', [location.isp, location.org, location.as, location.timezone].filter(Boolean).join(' · '), 'geo-note'));
      card.append(node('div', `Геоданные: ${date(location.last_success_at)}${address.stale ? ' · Данные устарели' : ''}`, address.stale ? 'geo-warning-text' : 'geo-note'));
    }
    if (service.error) card.append(node('p', service.error, 'geo-warning-text'));
    if (address?.error) card.append(node('p', `${address.ip}: ${address.error}`, 'geo-warning-text'));
    if (address?.location?.error) card.append(node('div', `Последняя попытка: ${date(address.location.last_attempt_at)}; повтор: ${date(address.location.next_attempt_at)}`, 'geo-note'));
    if (service.last_check) {
      const check = service.last_check;
      card.append(node('div', `Проверка: ${date(check.checked_at)} · ${check.response_time_ms} мс`, 'geo-note'));
      if (check.status !== 'online') {
        const details = node('details');
        details.append(node('summary', 'Диагностика проверки'));
        details.append(node('pre', JSON.stringify({ status: check.status, error: check.error, http_status: check.http_status, tls: check.tls, diagnostics: check.diagnostics }, null, 2)));
        card.append(details);
      }
    }
    card.append(detailButton(service));
    return card;
  }
  function fit() {
    if (map && bounds.length) map.fitBounds(bounds, { padding: [35, 35], maxZoom: 8, animate: false });
  }
  function init() {
    if (map || !window.L) return;
    map = L.map('serviceMap', { scrollWheelZoom: false }).setView([25, 15], 2);
    const tiles = L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
      maxZoom: 19,
      attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
    }).addTo(map);
    tiles.on('tileerror', () => {
      tileErrors++;
      element('geoError').textContent = 'Не удалось загрузить подложку OpenStreetMap. Расположения и диагностика доступны в списке ниже.';
      element('geoError').classList.remove('hidden');
    });
    tiles.on('tileload', () => {
      if (tileErrors) { tileErrors = 0; element('geoError').classList.add('hidden'); }
    });
    markers = L.layerGroup().addTo(map);
    map.on('resize', fit);
    element('geoFit').onclick = fit;
  }
  function render(items) {
    const root = element('geoLocations');
    root.replaceChildren();
    markers?.clearLayers();
    bounds = [];
    const groups = new Map();
    const missing = [];
    const placed = [];
    let placedCount = 0;
    for (const service of items) {
      let hasLocation = false;
      for (const address of service.addresses) {
        const location = address.location;
        if (!location?.last_success_at || !Number.isFinite(location.lat) || !Number.isFinite(location.lon)) {
          missing.push({ service, address });
          continue;
        }
        hasLocation = true;
        const key = `${location.lat},${location.lon}`;
        if (!groups.has(key)) groups.set(key, { coords: [location.lat, location.lon], entries: [] });
        groups.get(key).entries.push({ service, address });
        placed.push({ service, address });
      }
      if (hasLocation) placedCount++;
      if (!service.addresses.length) missing.push({ service });
    }
    for (const group of groups.values()) {
      bounds.push(group.coords);
      if (!map) continue;
      const popup = node('div', undefined, 'geo-popup');
      group.entries.forEach(({ service, address }) => popup.append(serviceInfo(service, address)));
      const tones = group.entries.map(entry => tone(entry.service));
      const worst = ['offline', 'warning', 'online', 'unknown'].find(value => tones.includes(value));
      const names = group.entries.map(entry => entry.service.name).join(', ');
      L.marker(group.coords, {
        title: names, alt: names, keyboard: true,
        icon: L.divIcon({ className: 'geo-pin', html: `<span style="border-color:${colors[worst]}">${group.entries.length > 1 ? group.entries.length : '●'}</span>`, iconSize: [32, 32], iconAnchor: [16, 16] })
      }).bindPopup(popup, { maxWidth: 380 }).addTo(markers);
    }
    element('geoSummary').textContent = items.length ? `На карте ${placedCount} из ${items.length} сервисов · ${groups.size} мест расположения` : 'Сервисов по выбранным фильтрам нет.';
    element('geoFit').disabled = !map || !bounds.length;
    if (missing.length) {
      root.append(node('h3', `Без геолокации (${missing.length})`));
      missing.forEach(({ service, address }) => root.append(serviceInfo(service, address)));
    }
    if (placed.length) {
      const details = node('details', undefined, 'geo-address-list');
      details.append(node('summary', `Расположения и диагностика (${placed.length})`));
      placed.forEach(({ service, address }) => details.append(serviceInfo(service, address)));
      root.append(details);
    }
    // Fit only when filters/addresses change, never on periodic status refresh.
    const nextSignature = JSON.stringify([...items].sort((a, b) => a.id - b.id).map(service => [service.id,
      [...service.addresses].sort((a, b) => a.ip.localeCompare(b.ip)).map(address => [address.ip, address.location?.lat, address.location?.lon])
    ]));
    if (nextSignature !== signature) { signature = nextSignature; fit(); }
  }
  function query() {
    const params = new URLSearchParams();
    const filters = { environmentFilter: 'environment', serviceSearch: 'search', typeFilter: 'check_type', tagFilter: 'tag', statusFilter: 'status', sortFilter: 'sort' };
    for (const [id, key] of Object.entries(filters)) {
      const value = element(id).value.trim();
      if (value) params.set(key, value);
    }
    return params;
  }
  async function refresh() {
    const current = ++generation;
    request?.abort();
    request = new AbortController();
    try {
      const data = await api('/services/map?' + activeQuery, { signal: request.signal });
      if (generation !== current) return;
      if (!tileErrors && window.L) element('geoError').classList.add('hidden');
      render(data.services || []);
    } catch (error) {
      if (error.name === 'AbortError' || generation !== current) return;
      element('geoError').textContent = `Не удалось обновить карту: ${error.message}. Показаны ранее загруженные данные; следующая попытка через 30 секунд.`;
      element('geoError').classList.remove('hidden');
    }
  }
  return {
    show() {
      activeQuery = query().toString();
      init();
      if (!window.L) {
        element('geoError').textContent = 'Библиотека карты не загрузилась. Расположения доступны в списке ниже.';
        element('geoError').classList.remove('hidden');
      }
      map?.invalidateSize({ pan: false });
      refresh();
      clearInterval(timer);
      timer = setInterval(refresh, 30000);
    },
    hide() { clearInterval(timer); request?.abort(); generation++; }
  };
})();
