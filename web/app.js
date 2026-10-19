const API = '/api/v1';
let items = [];
let editingId = null;

const $ = (id) => document.getElementById(id);

function toast(msg) { console.log(msg); }

async function api(path, opts = {}) {
  const res = await fetch(API + path, {
    headers: { 'Content-Type': 'application/json' },
    ...opts,
  });
  if (!res.ok) {
    const t = await res.text();
    throw new Error(t || res.statusText);
  }
  if (res.status === 204) return null;
  return res.json();
}

// ---------- Items ----------
async function loadItems() {
  const q = $('search').value.trim();
  const kind = $('kindFilter').value;
  const tag = $('tagFilter').value;
  const params = new URLSearchParams();
  if (q) params.set('q', q);
  if (kind) params.set('kind', kind);
  if (tag) params.set('tag', tag);
  params.set('limit', '200');
  const data = await api('/items?' + params.toString());
  items = data.items || [];
  renderList();
  await refreshTags();
}

function escapeHtml(s) {
  return (s || '').replace(/[&<>"']/g, (c) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
}

function renderList() {
  const list = $('list');
  if (!items.length) { list.innerHTML = '<div class="muted">Записей нет. Создайте первую.</div>'; return; }
  list.innerHTML = items.map(it => {
    const badge = it.kind;
    const lang = it.language ? `<span class="badge">${escapeHtml(it.language)}</span>` : '';
    const fav = it.favorite ? '<span class="fav">★</span>' : '';
    let body = '';
    if (it.kind === 'secret') {
      body = `<div class="card-body secret-value">[скрыто] <button data-action="reveal" data-id="${it.id}">Показать</button></div>`;
    } else {
      const rendered = it.rendered_html || escapeHtml(it.content || '');
      body = `<div class="card-body markdown-body">${rendered}</div>`;
    }
    const tags = (it.tags || []).map(t => `<span class="tag" data-tag="${escapeHtml(t)}">${escapeHtml(t)}</span>`).join('');
    return `
      <article class="card">
        <div class="card-header">
          <div class="card-title">${escapeHtml(it.title)} ${fav}</div>
          <div>${lang}<span class="badge">${badge}</span></div>
        </div>
        <div class="card-meta">обновлено: ${new Date(it.updated_at).toLocaleString()}</div>
        ${body}
        <div class="tags">${tags}</div>
        <div class="card-actions">
          <button data-action="edit" data-id="${it.id}">Редактировать</button>
          <button data-action="delete" data-id="${it.id}">Удалить</button>
        </div>
      </article>`;
  }).join('');

  list.querySelectorAll('button[data-action]').forEach(btn => {
    btn.addEventListener('click', () => {
      const id = Number(btn.dataset.id);
      const action = btn.dataset.action;
      if (action === 'edit') openEdit(id);
      else if (action === 'delete') deleteItem(id);
      else if (action === 'reveal') revealSecret(id);
    });
  });
  list.querySelectorAll('.tag[data-tag]').forEach(el => {
    el.addEventListener('click', () => {
      $('tagFilter').value = el.dataset.tag;
      loadItems();
    });
  });
}

async function refreshTags() {
  const data = await api('/tags');
  const sel = $('tagFilter');
  const cur = sel.value;
  sel.innerHTML = '<option value="">Все теги</option>';
  for (const t of (data.tags || [])) {
    const o = document.createElement('option');
    o.value = t; o.textContent = t;
    if (t === cur) o.selected = true;
    sel.appendChild(o);
  }
  const cloud = $('tagList');
  cloud.innerHTML = (data.tags || []).map(t => `<span class="tag" data-tag="${escapeHtml(t)}">${escapeHtml(t)}</span>`).join('');
  cloud.querySelectorAll('.tag').forEach(el => {
    el.addEventListener('click', () => { $('tagFilter').value = el.dataset.tag; loadItems(); });
  });
}

function openNew() {
  editingId = null;
  $('modalTitle').textContent = 'Новая запись';
  $('fKind').value = 'snippet';
  $('fTitle').value = '';
  $('fLang').value = '';
  $('fContent').value = '';
  $('fSecret').value = '';
  $('fTags').value = '';
  $('fFav').checked = false;
  $('modal').classList.remove('hidden');
}
function openEdit(id) {
  const it = items.find(x => x.id === id);
  if (!it) return;
  editingId = id;
  $('modalTitle').textContent = 'Редактирование';
  $('fKind').value = it.kind;
  $('fTitle').value = it.title;
  $('fLang').value = it.language || '';
  $('fContent').value = it.content || '';
  $('fSecret').value = '';
  $('fTags').value = (it.tags || []).join(', ');
  $('fFav').checked = !!it.favorite;
  $('modal').classList.remove('hidden');
}
$('btnNew').addEventListener('click', openNew);
$('modalClose').addEventListener('click', () => $('modal').classList.add('hidden'));
$('modalCancel').addEventListener('click', () => $('modal').classList.add('hidden'));

$('itemForm').addEventListener('submit', async (e) => {
  e.preventDefault();
  const payload = {
    kind: $('fKind').value,
    title: $('fTitle').value.trim(),
    language: $('fLang').value.trim(),
    content: $('fContent').value,
    tags: $('fTags').value.split(',').map(s => s.trim()).filter(Boolean),
    favorite: $('fFav').checked,
  };
  const secret = $('fSecret').value;
  if (secret) payload.secret = secret;
  try {
    if (editingId) {
      await api('/items/' + editingId, { method: 'PUT', body: JSON.stringify(payload) });
    } else {
      await api('/items', { method: 'POST', body: JSON.stringify(payload) });
    }
    $('modal').classList.add('hidden');
    await loadItems();
  } catch (err) {
    alert('Ошибка: ' + err.message);
  }
});

async function deleteItem(id) {
  if (!confirm('Удалить запись?')) return;
  await api('/items/' + id, { method: 'DELETE' });
  await loadItems();
}

async function revealSecret(id) {
  try {
    const data = await api('/items/' + id + '/reveal', { method: 'POST' });
    alert('Значение: ' + data.value);
  } catch (e) {
    alert('Не удалось показать секрет');
  }
}

$('search').addEventListener('input', debounce(loadItems, 300));
$('kindFilter').addEventListener('change', loadItems);
$('tagFilter').addEventListener('change', loadItems);

function debounce(fn, ms) {
  let t; return (...a) => { clearTimeout(t); t = setTimeout(() => fn(...a), ms); };
}

// init
(async function init() {
  await loadItems();
})();
