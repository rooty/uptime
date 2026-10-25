const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { join } = require('node:path');
const { test } = require('node:test');
const vm = require('node:vm');

class Element {
  constructor(tag) {
    this.tagName = tag;
    this.children = [];
    this.text = '';
    this.attributes = {};
    this.value = '';
    this.classes = new Set();
    this.classList = {
      add: value => this.classes.add(value),
      remove: value => this.classes.delete(value)
    };
  }
  set textContent(value) { this.text = String(value); this.children = []; }
  get textContent() { return this.text + this.children.map(child => child.textContent).join('\n'); }
  append(...children) { this.children.push(...children); }
  replaceChildren(...children) { this.text = ''; this.children = children; }
  setAttribute(name, value) { this.attributes[name] = value; }
}

function descendants(root, predicate) {
  return root.children.flatMap(child => [
    ...(predicate(child) ? [child] : []), ...descendants(child, predicate)
  ]);
}

function harness(initialItems) {
  let items = initialItems;
  const elements = new Map();
  const get = id => {
    if (!elements.has(id)) elements.set(id, new Element('div'));
    return elements.get(id);
  };
  const layer = { markers: [], addTo() { return this; }, clearLayers() { this.markers = []; } };
  const map = { fits: 0, setView() { return this; }, on() {}, invalidateSize() {}, fitBounds() { this.fits++; } };
  const leaflet = {
    map: () => map,
    tileLayer: () => ({ addTo() { return this; }, on() {} }),
    layerGroup: () => layer,
    divIcon: options => options,
    marker: (coords, options) => ({
      coords, options,
      bindPopup(popup) { this.popup = popup; return this; },
      addTo(target) { target.markers.push(this); return this; }
    })
  };
  let refresh;
  const sandbox = {
    document: { getElementById: get, createElement: tag => new Element(tag) },
    window: { L: leaflet }, L: leaflet, AbortController, URLSearchParams,
    api: async () => ({ services: items }),
    statusLabel: check => [check?.status || 'unknown', check?.status || 'Нет проверки'],
    openDetail: async () => {},
    setInterval: callback => { refresh = callback; return 1; }, clearInterval: () => {}
  };
  const source = readFileSync(join(__dirname, 'ops-map.js'), 'utf8');
  const controller = vm.runInNewContext(source + '\nOpsMap;', sandbox, { filename: 'ops-map.js' });
  return {
    layer, map, get,
    async render(nextItems = items) {
      items = nextItems;
      controller.show();
      await new Promise(setImmediate);
      assert.equal(get('geoError').textContent, '', 'map rendering failed');
    },
    async update(nextItems) { items = nextItems; await refresh(); },
    stop: () => controller.hide()
  };
}

function address(ip, lat = 51.1, lon = 17.03) {
  return { ip, status: 'success', location: { lat, lon, last_success_at: '2026-10-25T22:00:00Z', city: 'Test city' } };
}

function service(id, addresses, name = 'GitHub Page', status = 'online') {
  return { id, name, hostname: 'example.test', enabled: true, addresses, last_check: { status, checked_at: '2026-10-25T22:00:00Z', response_time_ms: 5 } };
}

const cards = popup => descendants(popup, node => node.className === 'geo-service-info');
const buttons = popup => descendants(popup, node => node.tagName === 'button');
const markerCount = marker => Number(marker.options.icon.html.match(/>(\d+)<\/span>/)[1]);

test('four colocated IPs of one service count as four monitoring items', async () => {
  const ips = ['185.199.108.153', '185.199.109.153', '185.199.110.153', '185.199.111.153'];
  const view = harness([service(1, ips.map(ip => address(ip)))]);
  await view.render();
  assert.equal(view.layer.markers.length, 1);
  const marker = view.layer.markers[0];
  assert.equal(markerCount(marker), 4);
  assert.equal(cards(marker.popup).length, 1);
  assert.equal(buttons(marker.popup).length, 1);
  assert.match(marker.options.title, /Элементов мониторинга \(IP → сервис\): 4/);
  for (const ip of ips) assert.ok(marker.popup.textContent.includes(ip), `lost IP ${ip}`);
  assert.match(view.get('geoSummary').textContent, /На карте 1 из 1 сервисов/);
  assert.match(view.get('geoLocations').textContent, /Расположения и диагностика \(1\)/);
  view.stop();
});

test('pairs sharing IP or service name are distinguished by service ID', async () => {
  const shared = address('8.8.8.8');
  const view = harness([
    service(1, [shared, address('1.1.1.1')]),
    service(2, [shared], 'GitHub Page', 'offline')
  ]);
  await view.render();
  const marker = view.layer.markers[0];
  assert.equal(markerCount(marker), 3);
  assert.equal(cards(marker.popup).length, 2);
  assert.equal(buttons(marker.popup).length, 2);
  assert.match(marker.options.icon.html, /#ef5367/);
  view.stop();
});

test('one shared IP with two different services counts as two monitoring items', async () => {
  const view = harness([service(1, [address('8.8.8.8')]), service(2, [address('8.8.8.8')])]);
  await view.render();
  assert.equal(view.layer.markers.length, 1);
  assert.equal(markerCount(view.layer.markers[0]), 2);
  assert.equal(cards(view.layer.markers[0].popup).length, 2);
  view.stop();
});

test('pairs of one service in different locations count at their own coordinates', async () => {
  const view = harness([service(1, [address('8.8.8.8'), address('1.1.1.1', 40, -74)])]);
  await view.render();
  assert.equal(view.layer.markers.length, 2);
  for (const marker of view.layer.markers) {
    assert.equal(markerCount(marker), 1);
    assert.equal(cards(marker.popup).length, 1);
    assert.equal(descendants(marker.popup, node => node.className === 'geo-address-info').length, 1);
  }
  assert.match(view.get('geoSummary').textContent, /На карте 1 из 1 сервисов · 2 мест/);
  assert.match(view.get('geoLocations').textContent, /Расположения и диагностика \(1\)/);
  view.stop();
});

test('deduplication preserves per-IP errors, stale data and common check diagnostics', async () => {
  const first = address('8.8.8.8');
  first.stale = true;
  first.error = 'ip-api.com IP 8.8.8.8: HTTP status 429';
  first.location.error = first.error;
  first.location.last_attempt_at = '2026-10-25T23:00:00Z';
  first.location.next_attempt_at = '2026-10-25T23:01:00Z';
  const item = service(1, [first, address('1.1.1.1')], 'TLS <test>', 'tls_error');
  item.error = 'DNS lookup example.test: timeout';
  item.last_check.error = 'certificate validation failed';
  item.last_check.diagnostics = { phase: 'TLS handshake' };
  const view = harness([item]);
  await view.render();
  const marker = view.layer.markers[0];
  assert.equal(markerCount(marker), 2);
  assert.equal(cards(marker.popup).length, 1);
  for (const text of ['8.8.8.8', '1.1.1.1', 'HTTP status 429', 'Данные устарели', 'DNS lookup', 'certificate validation failed', 'TLS handshake', 'Последняя попытка']) assert.ok(marker.popup.textContent.includes(text), `lost diagnostic ${text}`);
  assert.match(marker.options.icon.html, /#f0b84b/);
  view.stop();
});

test('missing coordinates are grouped per service without inflating map counts', async () => {
  const view = harness([service(1, [
    address('8.8.8.8'),
    { ip: '10.0.0.1', error: 'private IP' },
    { ip: '10.0.0.2', error: 'private IP' }
  ])]);
  await view.render();
  assert.equal(markerCount(view.layer.markers[0]), 1);
  assert.match(view.get('geoLocations').textContent, /Без геолокации \(1\)/);
  assert.ok(view.get('geoLocations').textContent.includes('10.0.0.1'));
  assert.ok(view.get('geoLocations').textContent.includes('10.0.0.2'));
  view.stop();
});

test('duplicate IPs do not duplicate details and periodic status updates do not refit', async () => {
  const item = service(1, [address('8.8.8.8'), address('8.8.8.8')]);
  const view = harness([item]);
  await view.render();
  const fits = view.map.fits;
  assert.equal(markerCount(view.layer.markers[0]), 1);
  assert.equal(descendants(view.layer.markers[0].popup, node => node.className === 'geo-address-info').length, 1);
  item.last_check.status = 'offline';
  await view.update([item]);
  assert.equal(view.map.fits, fits);
  assert.match(view.layer.markers[0].options.icon.html, /#ef5367/);
  view.stop();
});
