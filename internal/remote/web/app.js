'use strict';
// The web remote page. The first half is pure (page state, interpolation,
// the API wrapper, the back-off) and is tested with node; the second half
// is the DOM and only runs in a browser.

// ---- pure ----

function initial() {
  return { state: null, at: 0, queue: { index: -1, songs: [] }, connected: false, full: false };
}

// reduce applies one event to the page state and returns the new state.
// Events: {type:'state', data, now}, {type:'queue', data}, {type:'conn', up, full}.
function reduce(s, ev) {
  switch (ev.type) {
    case 'state': {
      const q = ev.data.index === s.queue.index ? s.queue : { index: ev.data.index, songs: s.queue.songs };
      return Object.assign({}, s, { state: ev.data, at: ev.now || 0, queue: q });
    }
    case 'queue':
      return Object.assign({}, s, { queue: { index: ev.data.index, songs: ev.data.songs || [] } });
    case 'conn':
      return Object.assign({}, s, { connected: ev.up, full: !!ev.full });
  }
  return s;
}

// position is the song position in ms at time now, moved on from the last
// tick while playing.
function position(s, now) {
  const st = s.state;
  if (!st) return 0;
  let p = st.position_ms;
  if (st.status === 'playing') p += Math.max(0, now - s.at);
  return st.duration_ms > 0 ? Math.min(p, st.duration_ms) : p;
}

// backoff is the wait before reconnect number n (from 0): 1, 2, 4 ... 30 s.
function backoff(n) { return Math.min(30000, 1000 * Math.pow(2, n)); }

// retryDelay is the wait before reconnect number n. A server with all its
// slots taken is asked again every 30 s, not faster.
function retryDelay(n, full) { return full ? 30000 : backoff(n); }

const bannerText = (s) => (s.full ? 'Too many open remotes' : 'Reconnecting…');

// The server sends a ping every 15 s on an otherwise quiet stream; a stream
// that has said nothing for this long is dead (a phone that left the wifi
// never gets an error from EventSource).
const WATCHDOG_MS = 40000;
function makeWatchdog(clock) {
  let last = clock();
  return { heard() { last = clock(); }, stale() { return clock() - last > WATCHDOG_MS; } };
}

// splitPath decodes the parts of a browse path, or gives null when the hash
// holds a bad escape (the page then shows the Browse home).
function splitPath(rest) {
  try { return rest.split('/').map(decodeURIComponent); } catch (e) { return null; }
}

const albumsURL = (list, offset) => '/api/albums?list=' + encodeURIComponent(list) + '&offset=' + offset;

function fmtTime(ms) {
  const t = Math.floor(Math.max(0, ms || 0) / 1000);
  const h = Math.floor(t / 3600), m = Math.floor(t / 60) % 60, sec = t % 60;
  const ss = (sec < 10 ? '0' : '') + sec;
  return h > 0 ? h + ':' + (m < 10 ? '0' : '') + m + ':' + ss : m + ':' + ss;
}

// makeApi wraps fetch: JSON in and out, and one error type with the status
// (0 when the network failed) and the server's message.
function makeApi(fetchFn) {
  const fail = (status, message) => Object.assign(new Error(message), { status, stale: status === 409 });
  async function call(url, opt) {
    let r;
    try { r = await fetchFn(url, opt); } catch (e) { throw fail(0, 'No connection'); }
    let body = null;
    try { body = await r.json(); } catch (e) { /* not JSON */ }
    if (!r.ok) throw fail(r.status, body && body.error ? body.error : 'Request failed (' + r.status + ')');
    return body;
  }
  return {
    get: (url) => call(url),
    post: (url, body) => call(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
  };
}

const plural = (n, w) => n + ' ' + w + (n === 1 ? '' : 's');

function addedMessage(how, n) {
  if (how === 'next') return 'Playing next';
  if (how === 'now') return 'Playing';
  return 'Added ' + plural(n, 'song');
}

// moveTarget is the {from, to} of moving row i one step (dir -1 or 1), or null at the ends.
function moveTarget(i, n, dir) {
  const to = i + dir;
  return to < 0 || to >= n ? null : { from: i, to };
}

function parseHash(h) {
  const parts = (h || '').replace(/^#/, '').split('/');
  const tab = ['now', 'queue', 'browse', 'search'].includes(parts[0]) ? parts[0] : 'now';
  return { tab, rest: tab === parts[0] ? parts.slice(1).join('/') : '' };
}

if (typeof module !== 'undefined') {
  module.exports = { initial, reduce, position, backoff, retryDelay, bannerText, WATCHDOG_MS, makeWatchdog, splitPath, albumsURL, fmtTime, makeApi, addedMessage, plural, moveTarget, parseHash };
}

// ---- the page ----

if (typeof document !== 'undefined') (function () {
  const api = makeApi((u, o) => fetch(u, o));
  const $ = (id) => document.getElementById(id);
  let S = initial();

  const ICONS = {
    play: 'M8 5v14l11-7z', pause: 'M6 19h4V5H6v14zm8-14v14h4V5h-4z',
    next: 'M6 18l8.5-6L6 6v12zM16 6v12h2V6h-2z', prev: 'M6 6h2v12H6zm3.5 6l8.5 6V6z',
    volume: 'M3 9v6h4l5 5V4L7 9H3zm13.5 3A4.5 4.5 0 0 0 14 7.97v8.05c1.48-.73 2.5-2.25 2.5-4.02z',
    mute: 'M16.5 12A4.5 4.5 0 0 0 14 7.97v2.21l2.45 2.45c.03-.2.05-.41.05-.63zM4.27 3L3 4.27 7.73 9H3v6h4l5 5v-6.73l4.25 4.25c-.67.52-1.42.93-2.25 1.18v2.06a8.99 8.99 0 0 0 3.69-1.81L19.73 21 21 19.73l-9-9L4.27 3zM12 4L9.91 6.09 12 8.18V4z',
    star: 'M12 17.27L18.18 21l-1.64-7.03L22 9.24l-7.19-.61L12 2 9.19 8.63 2 9.24l5.46 4.73L5.82 21z',
    shuffle: 'M10.59 9.17L5.41 4 4 5.41l5.17 5.17 1.42-1.41zM14.5 4l2.04 2.04L4 18.59 5.41 20 17.96 7.46 20 9.5V4h-5.5zm.33 9.41l-1.41 1.41 3.13 3.13L14.5 20H20v-5.5l-2.04 2.04-3.13-3.13z',
    repeat: 'M7 7h10v3l4-4-4-4v3H5v6h2V7zm10 10H7v-3l-4 4 4 4v-3h12v-6h-2v4z',
    more: 'M6 10a2 2 0 1 0 0 4 2 2 0 0 0 0-4zm12 0a2 2 0 1 0 0 4 2 2 0 0 0 0-4zm-6 0a2 2 0 1 0 0 4 2 2 0 0 0 0-4z',
    note: 'M12 3v10.55A4 4 0 1 0 14 17V7h4V3h-6z',
    queue: 'M3 13h2v-2H3v2zm0 4h2v-2H3v2zm0-8h2V7H3v2zm4 4h14v-2H7v2zm0 4h14v-2H7v2zM7 7v2h14V7H7z',
    browse: 'M4 6H2v14c0 1.1.9 2 2 2h14v-2H4V6zm16-4H8c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2zm-1 9H9V9h10v2zm-4 4H9v-2h6v2zm4-8H9V5h10v2z',
    search: 'M15.5 14h-.79l-.28-.27A6.47 6.47 0 0 0 16 9.5 6.5 6.5 0 1 0 9.5 16c1.61 0 3.09-.59 4.23-1.57l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0C7.01 14 5 11.99 5 9.5S7.01 5 9.5 5 14 7.01 14 9.5 11.99 14 9.5 14z',
    back: 'M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z',
  };
  function svg(name) {
    const NS = 'http://www.w3.org/2000/svg';
    const s = document.createElementNS(NS, 'svg'), p = document.createElementNS(NS, 'path');
    s.setAttribute('viewBox', '0 0 24 24');
    s.setAttribute('aria-hidden', 'true');
    p.setAttribute('d', ICONS[name]);
    s.appendChild(p);
    return s;
  }
  function setIcon(el, name) { el.replaceChildren(svg(name), ...Array.from(el.children).filter((c) => c.tagName === 'B')); }

  // h builds an element. Text is always set as text, never as HTML.
  function h(tag, props, ...kids) {
    const el = document.createElement(tag);
    for (const k in props || {}) {
      const v = props[k];
      if (v == null || v === false) continue;
      if (k === 'class') el.className = v;
      else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
      else el.setAttribute(k, v === true ? '' : v);
    }
    for (const k of kids.flat()) if (k != null && k !== false) el.append(k);
    return el;
  }
  const iconBtn = (name, label, fn, cls) => {
    const b = h('button', { class: cls || 'more', 'aria-label': label, onclick: fn });
    b.append(svg(name));
    return b;
  };

  // ---- feedback ----
  function toast(msg, err) {
    const t = h('div', { class: 'toast' + (err ? ' err' : '') }, msg);
    $('toasts').append(t);
    while ($('toasts').children.length > 3) $('toasts').firstChild.remove();
    setTimeout(() => t.remove(), 2600);
  }
  function fail(e) {
    if (e.stale) { toast('The queue changed', true); loadQueue(); } else toast(e.message, true);
  }
  async function attempt(fn) { try { return await fn(); } catch (e) { fail(e); } }
  const cmd = (body) => attempt(() => api.post('/api/cmd', body));
  const coverURL = (id, size) => '/api/cover/' + encodeURIComponent(id) + '?size=' + size;

  async function play(body) {
    const r = await attempt(() => api.post('/api/play', body));
    if (r) toast(r.added ? addedMessage(body.how, r.added) : 'Nothing to add');
  }

  // ---- state ----
  function dispatch(ev) {
    const prev = S;
    S = reduce(S, ev);
    if (ev.type === 'state') {
      renderNow();
      if (prev.queue.index !== S.queue.index) markCurrent();
    } else if (ev.type === 'queue') {
      renderQueue();
    } else if (ev.type === 'conn') {
      $('banner').hidden = ev.up;
      $('banner').textContent = bannerText(S);
    }
  }
  const now = () => performance.now();
  async function loadState() { const st = await attempt(() => api.get('/api/state')); if (st) dispatch({ type: 'state', data: st, now: now() }); }
  async function loadQueue() { const q = await attempt(() => api.get('/api/queue')); if (q) dispatch({ type: 'queue', data: q }); }

  // The event stream. It counts as up at its first state or queue event (a
  // full server answers 200 and then a "full" event). It is dropped when it
  // errors, when the server says it is full, or when nothing at all (not even
  // a ping) has arrived for WATCHDOG_MS.
  let es = null, tries = 0, wasDown = false, retrying = false, isUp = false, retryTimer = 0;
  const dog = makeWatchdog(now);
  function down(full) {
    if (es) es.close();
    es = null;
    wasDown = true;
    isUp = false;
    dispatch({ type: 'conn', up: false, full });
    clearTimeout(retryTimer);
    retrying = true;
    retryTimer = setTimeout(connect, retryDelay(tries++, full));
  }
  function connect() {
    retrying = false;
    dog.heard();
    const mine = es = new EventSource('/api/events');
    const up = () => {
      dog.heard();
      if (isUp) return;
      isUp = true;
      tries = 0;
      dispatch({ type: 'conn', up: true });
      if (wasDown) { wasDown = false; artistsCache = null; loadState(); loadQueue(); } // the server may be another one
    };
    mine.addEventListener('state', (e) => { up(); dispatch({ type: 'state', data: JSON.parse(e.data), now: now() }); });
    mine.addEventListener('queue', (e) => { up(); dispatch({ type: 'queue', data: JSON.parse(e.data) }); });
    mine.addEventListener('ping', () => dog.heard());
    mine.addEventListener('full', () => { if (es === mine) down(true); });
    mine.onerror = () => { if (es === mine) down(false); };
  }
  setInterval(() => { if (es && !retrying && dog.stale()) down(false); }, 5000);

  // ---- Now Playing ----
  let coverID = null, dragging = null, volTimer = 0, volHold = 0;
  function renderNow() {
    const st = S.state, song = st && st.song;
    $('idle').hidden = !!song;
    $('player').hidden = !song;
    document.title = song ? song.title + ' · ' + song.artist : 'MiSTer Subsonic';
    if (!st) return;
    if (song) {
      $('title').textContent = song.title;
      $('artist').textContent = song.artist;
      $('album').textContent = song.album;
      if (song.cover_id !== coverID) {
        coverID = song.cover_id;
        const img = $('coverimg');
        img.hidden = true;
        if (coverID) img.src = coverURL(coverID, 500);
      }
      $('dur').textContent = fmtTime(st.duration_ms);
      $('bar').setAttribute('aria-valuemax', st.duration_ms);
      $('b-star').classList.toggle('on', song.starred);
      $('b-star').setAttribute('aria-pressed', song.starred);
    }
    setIcon($('b-toggle'), st.status === 'playing' ? 'pause' : 'play');
    $('b-shuffle').classList.toggle('on', st.shuffle);
    $('b-repeat').classList.toggle('on', st.repeat !== 'off');
    $('rep1').hidden = st.repeat !== 'one';
    $('b-mute').classList.toggle('dimmed', st.muted);
    setIcon($('b-mute'), st.muted ? 'mute' : 'volume');
    document.querySelector('.vol').classList.toggle('muted', st.muted);
    if (!volHold) setVolume(st.volume_db);
    drawPosition();
  }
  function setVolume(db) {
    $('volume').value = db;
    $('voltxt').textContent = Math.round(db) + ' dB';
  }
  function drawPosition() {
    const st = S.state;
    if (!st || !st.song || dragging != null) return;
    const p = position(S, now());
    showPosition(p, st.duration_ms);
  }
  function showPosition(p, d) {
    const f = d > 0 ? Math.min(1, p / d) * 100 : 0;
    $('fill').style.width = f + '%';
    $('knob').style.left = f + '%';
    $('pos').textContent = fmtTime(p);
    $('bar').setAttribute('aria-valuenow', Math.round(p));
  }
  function seekTo(ms) {
    const st = S.state;
    if (!st || !st.song) return;
    ms = Math.round(Math.max(0, Math.min(ms, st.duration_ms)));
    dispatch({ type: 'state', data: Object.assign({}, st, { position_ms: ms }), now: now() });
    cmd({ do: 'seek', position_ms: ms });
  }
  function wireNow() {
    $('coverimg').addEventListener('load', () => { $('coverimg').hidden = false; });
    $('coverimg').addEventListener('error', () => { $('coverimg').hidden = true; });
    const bar = $('bar');
    const at = (e) => {
      const r = bar.getBoundingClientRect();
      return Math.max(0, Math.min(1, (e.clientX - r.left) / r.width)) * (S.state ? S.state.duration_ms : 0);
    };
    bar.addEventListener('pointerdown', (e) => {
      if (!S.state || !S.state.song) return;
      bar.setPointerCapture(e.pointerId);
      dragging = at(e);
      showPosition(dragging, S.state.duration_ms);
    });
    bar.addEventListener('pointermove', (e) => {
      if (dragging == null) return;
      dragging = at(e);
      showPosition(dragging, S.state.duration_ms);
    });
    bar.addEventListener('pointerup', (e) => {
      if (dragging == null) return;
      const ms = at(e);
      dragging = null;
      seekTo(ms);
    });
    bar.addEventListener('pointercancel', () => { dragging = null; });
    bar.addEventListener('keydown', (e) => {
      const d = { ArrowLeft: -5000, ArrowRight: 5000 }[e.key];
      if (d == null || !S.state) return;
      e.preventDefault();
      seekTo(position(S, now()) + d);
    });
    $('b-toggle').onclick = () => cmd({ do: 'toggle' });
    $('b-prev').onclick = () => cmd({ do: 'prev' });
    $('b-next').onclick = () => cmd({ do: 'next' });
    $('b-star').onclick = () => S.state && S.state.song && cmd({ do: 'star', on: !S.state.song.starred });
    $('b-shuffle').onclick = () => S.state && cmd({ do: 'shuffle', on: !S.state.shuffle });
    $('b-repeat').onclick = () => S.state && cmd({ do: 'repeat', mode: { off: 'all', all: 'one', one: 'off' }[S.state.repeat] || 'off' });
    $('b-mute').onclick = () => S.state && cmd({ do: 'mute', on: !S.state.muted });
    $('volume').addEventListener('input', (e) => {
      const db = Number(e.target.value);
      setVolume(db);
      volHold = Date.now();
      if (!volTimer) volTimer = setTimeout(() => { volTimer = 0; cmd({ do: 'volume', db: Number($('volume').value) }); }, 80);
    });
    // Keep the slider where the finger left it until the server has caught up.
    $('volume').addEventListener('change', () => setTimeout(() => { volHold = 0; }, 600));
    setInterval(() => { if (!document.hidden) drawPosition(); }, 250);
  }

  // ---- rows and menus ----
  function thumb(coverId, size) {
    const t = h('div', { class: 'thumb' });
    if (coverId) {
      const img = h('img', { src: coverURL(coverId, size || 96), alt: '', loading: 'lazy' });
      img.addEventListener('error', () => img.remove());
      t.append(img);
    }
    return t;
  }
  function closeMenus(except) {
    document.querySelectorAll('li.open').forEach((l) => { if (l !== except) l.classList.remove('open'); });
  }
  // item is one list entry: a row (a link, or a button when it has onTap),
  // plus a ⋯ button that opens its menu of {label, fn}.
  function item(o) {
    const li = h('li', { class: o.cur ? 'cur' : null });
    const inner = [
      thumb(o.cover),
      h('div', { class: 'txt' }, h('div', { class: 't1' }, o.title), o.sub ? h('div', { class: 't2' }, o.sub) : null),
      o.end ? h('div', { class: 'end' }, o.end) : null,
    ];
    const toggle = () => { closeMenus(li); li.classList.toggle('open'); };
    const row = o.href ? h('a', { class: 'row', href: o.href }, inner)
      : h('div', { class: 'row', role: 'button', tabindex: 0, onclick: o.onTap || toggle,
        onkeydown: (e) => { if (e.key === 'Enter') (o.onTap || toggle)(); } }, inner);
    const top = h('div', { class: 'toprow' }, row);
    if (o.menu) {
      top.append(iconBtn('more', 'More', toggle));
      li.append(top, h('div', { class: 'menu' }, o.menu.map((m) =>
        h('button', { class: 'btn' + (m.danger ? ' danger' : ''), onclick: () => { li.classList.remove('open'); m.fn(); } }, m.label))));
    } else li.append(top);
    return li;
  }
  const playMenu = (what, id) => [
    { label: 'Play', fn: () => play({ what, id, how: 'now' }) },
    { label: 'Play next', fn: () => play({ what, id, how: 'next' }) },
    { label: 'Add to queue', fn: () => play({ what, id, how: 'end' }) },
  ];
  // songMenu: Play starts from this song when it sits in an album or a playlist.
  const songMenu = (song, ctx, i) => [
    { label: 'Play', fn: () => play(ctx ? { what: ctx.what, id: ctx.id, start: i, how: 'now' } : { what: 'songs', ids: [song.id], how: 'now' }) },
    { label: 'Play next', fn: () => play({ what: 'songs', ids: [song.id], how: 'next' }) },
    { label: 'Add to queue', fn: () => play({ what: 'songs', ids: [song.id], how: 'end' }) },
  ];
  const songItems = (songs, ctx) => songs.map((s, i) =>
    item({ cover: s.cover_id, title: s.title, sub: s.artist + ' · ' + s.album, end: fmtTime(s.duration_ms), menu: songMenu(s, ctx, i) }));
  const bar = (what, id) => h('div', { class: 'bar' }, [['Play', 'now'], ['Play next', 'next'], ['Add to queue', 'end']].map(([label, how], i) =>
    h('button', { class: 'btn' + (i === 0 ? ' primary' : ''), onclick: () => play({ what, id, how }) }, label)));

  // ---- Queue ----
  let openRow = null, confirming = false, shownIndex = -2;
  function renderQueue() {
    const q = S.queue, root = $('queue');
    const list = h('ul', { class: 'list' });
    q.songs.forEach((s, i) => {
      const menu = [
        { label: 'Remove', danger: true, fn: () => cmd({ do: 'remove', index: i, song_id: s.id }) },
        { label: 'Move up', fn: () => moveRow(i, -1) },
        { label: 'Move down', fn: () => moveRow(i, 1) },
      ];
      const li = item({ cover: s.cover_id, title: s.title, sub: s.artist, end: fmtTime(s.duration_ms), cur: i === q.index, menu,
        onTap: () => cmd({ do: 'jump', index: i, song_id: s.id }) });
      li.dataset.i = i;
      if (openRow === i + ':' + s.id) li.classList.add('open');
      li.querySelector('.more').addEventListener('click', () => { openRow = li.classList.contains('open') ? i + ':' + s.id : null; });
      list.append(li);
    });
    const clear = confirming
      ? h('span', { class: 'confirm' }, 'Clear the queue?',
        h('button', { class: 'btn danger', onclick: () => { confirming = false; cmd({ do: 'clear' }); renderQueue(); } }, 'Clear'),
        h('button', { class: 'btn', onclick: () => { confirming = false; renderQueue(); } }, 'Cancel'))
      : h('button', { class: 'btn', onclick: () => { confirming = true; renderQueue(); } }, 'Clear queue');
    root.replaceChildren(
      h('div', { class: 'head' }, h('h2', null, 'Queue'), q.songs.length ? clear : null),
      q.songs.length ? list : h('div', { class: 'empty' }, 'The queue is empty'));
    shownIndex = q.index;
  }
  function moveRow(i, dir) {
    const t = moveTarget(i, S.queue.songs.length, dir);
    if (t) cmd({ do: 'move', from: t.from, to: t.to, song_id: S.queue.songs[i].id });
  }
  // markCurrent moves the highlight when only the index changed.
  function markCurrent() {
    document.querySelectorAll('#queue li.cur').forEach((l) => l.classList.remove('cur'));
    const li = document.querySelector('#queue li[data-i="' + S.queue.index + '"]');
    if (li) li.classList.add('cur');
  }

  // ---- Browse ----
  let nav = 0, artistsCache = null;
  const enc = encodeURIComponent;
  const albumCard = (a) => h('a', { class: 'card', href: '#browse/album/' + enc(a.id) },
    thumb(a.cover_id, 200), h('div', { class: 't1' }, a.name), h('div', { class: 't2' }, [a.artist, a.year || ''].filter(Boolean).join(' · ')));
  const head = (title, back, extra) => h('div', { class: 'head' },
    back ? h('a', { class: 'back', href: back, 'aria-label': 'Back' }, svg('back')) : null, h('h2', null, title), extra);
  const msg = (cls, text) => h('div', { class: cls }, text);

  async function renderBrowse(rest) {
    const my = ++nav, root = $('browse');
    const p = splitPath(rest) || [''];
    const show = (...kids) => { if (my === nav) { root.replaceChildren(...kids); } };
    const get = async (url) => {
      try { return await api.get(url); } catch (e) { show(head('Browse', '#browse'), msg('error', e.message)); return null; }
    };
    switch (p[0]) {
      case 'artists': {
        if (!artistsCache) artistsCache = await get('/api/artists');
        if (!artistsCache) { artistsCache = null; return; }
        const groups = artistsCache.filter((g) => g.artists.length);
        const cur = groups.find((g) => g.letter === p[1]) || groups[0];
        show(head('Artists', '#browse'),
          h('div', { class: 'letters' }, groups.map((g) => h('a', { href: '#browse/artists/' + enc(g.letter), class: g === cur ? 'on' : null }, g.letter))),
          cur ? h('ul', { class: 'list' }, cur.artists.map((a) => item({
            cover: a.cover_id, title: a.name, sub: plural(a.album_count, 'album'), href: '#browse/artist/' + enc(a.id) }))) : msg('empty', 'No artists'));
        return;
      }
      case 'artist': {
        const a = await get('/api/artist/' + enc(p[1]));
        if (a) show(head(a.name, '#browse/artists'), bar('artist', a.id), a.albums.length ? h('div', { class: 'grid' }, a.albums.map(albumCard)) : msg('empty', 'No albums'));
        return;
      }
      case 'albums': case 'genre': {
        const isGenre = p[0] === 'genre', list = isGenre ? 'genre' : p[1] || 'recent';
        const url = (o) => (isGenre ? '/api/genre/' + enc(p[1]) + '?offset=' + o : albumsURL(list, o));
        const grid = h('div', { class: 'grid' }), more = h('button', { class: 'btn', hidden: true }, 'More');
        let off = 0;
        const load = async () => {
          more.disabled = true;
          let r;
          try { r = await api.get(url(off)); } catch (e) { more.disabled = false; return toast(e.message, true); }
          if (my !== nav) return;
          off += r.albums.length;
          grid.append(...r.albums.map(albumCard));
          more.hidden = !r.more;
          more.disabled = false;
          if (!off) grid.replaceWith(msg('empty', 'No albums'));
        };
        more.onclick = load;
        show(isGenre ? head(p[1], '#browse/genres') : head('Albums', '#browse'),
          isGenre ? null : h('div', { class: 'seg' }, [['recent', 'Recent'], ['random', 'Random'], ['newest', 'Newest']].map(([k, label]) =>
            h('a', { href: '#browse/albums/' + k, class: k === list ? 'on' : null }, label))),
          grid, h('div', { class: 'bar' }, more));
        await load();
        return;
      }
      case 'album': {
        const a = await get('/api/album/' + enc(p[1]));
        if (a) show(head(a.name, a.artist_id ? '#browse/artist/' + enc(a.artist_id) : '#browse'),
          h('p', { class: 'dim' }, [a.artist, a.year || ''].filter(Boolean).join(' · ')), bar('album', a.id),
          h('ul', { class: 'list' }, songItems(a.songs, { what: 'album', id: a.id })));
        return;
      }
      case 'playlists': {
        const l = await get('/api/playlists');
        if (l) show(head('Playlists', '#browse'), l.length ? h('ul', { class: 'list' }, l.map((x) => item({
          cover: x.cover_id, title: x.name, sub: plural(x.song_count, 'song'), href: '#browse/playlist/' + enc(x.id), menu: playMenu('playlist', x.id) }))) : msg('empty', 'No playlists'));
        return;
      }
      case 'playlist': {
        const pl = await get('/api/playlist/' + enc(p[1]));
        if (pl) show(head(pl.name, '#browse/playlists'), bar('playlist', pl.id), h('ul', { class: 'list' }, songItems(pl.songs, { what: 'playlist', id: pl.id })));
        return;
      }
      case 'starred': {
        const r = await get('/api/starred');
        if (r) show(head('Starred', '#browse'), foundBlock(r, 'Nothing starred yet'));
        return;
      }
      case 'genres': {
        const l = await get('/api/genres');
        if (l) show(head('Genres', '#browse'), l.length ? h('ul', { class: 'list' }, l.map((g) => item({
          title: g.name, sub: plural(g.album_count, 'album') + ' · ' + plural(g.song_count, 'song'), href: '#browse/genre/' + enc(g.name) }))) : msg('empty', 'No genres'));
        return;
      }
    }
    const tile = (href, label, sub) => h('a', { class: 'tile', href }, label, h('span', null, sub));
    show(head('Browse'), h('div', { class: 'tiles' },
      tile('#browse/artists', 'Artists', 'A to Z'), tile('#browse/albums/recent', 'Albums', 'Recent, random, newest'),
      tile('#browse/playlists', 'Playlists', 'Your playlists'), tile('#browse/starred', 'Starred', 'Your favourites'),
      tile('#browse/genres', 'Genres', 'By genre')));
  }

  // foundBlock lists search or starred results, grouped.
  function foundBlock(r, none) {
    if (!r.artists.length && !r.albums.length && !r.songs.length) return msg('empty', none);
    const group = (title, items) => items.length ? [h('div', { class: 'sub' }, title), h('ul', { class: 'list' }, items)] : [];
    return h('div', null,
      group('Artists', r.artists.map((a) => item({ cover: a.cover_id, title: a.name, sub: plural(a.album_count, 'album'), href: '#browse/artist/' + enc(a.id), menu: playMenu('artist', a.id) }))),
      group('Albums', r.albums.map((a) => item({ cover: a.cover_id, title: a.name, sub: a.artist, href: '#browse/album/' + enc(a.id), menu: playMenu('album', a.id) }))),
      group('Songs', songItems(r.songs, null)));
  }

  // ---- Search ----
  let searchTimer = 0, searchSeq = 0;
  function wireSearch() {
    $('q').addEventListener('input', () => {
      clearTimeout(searchTimer);
      searchTimer = setTimeout(runSearch, 300);
    });
  }
  async function runSearch() {
    const q = $('q').value.trim(), my = ++searchSeq, out = $('results');
    if (q.length < 2) { out.replaceChildren(q ? msg('empty', 'Type at least 2 characters') : ''); return; }
    try {
      const r = await api.get('/api/search?q=' + enc(q));
      if (my === searchSeq) out.replaceChildren(foundBlock(r, 'Nothing found'));
    } catch (e) {
      if (my === searchSeq) out.replaceChildren(msg('error', e.message));
    }
  }

  // ---- routing ----
  function route() {
    const r = parseHash(location.hash);
    document.body.dataset.tab = r.tab;
    closeMenus();
    if (r.tab === 'browse') renderBrowse(r.rest);
    if (r.tab === 'queue' || (r.tab === 'now' && matchMedia('(min-width: 900px)').matches)) {
      const cur = document.querySelector('#queue li.cur');
      if (cur) cur.scrollIntoView({ block: 'center' });
    }
    if (r.tab === 'search' && matchMedia('(min-width: 900px)').matches) $('q').focus();
  }

  document.querySelectorAll('[data-icon]').forEach((el) => setIcon(el, el.dataset.icon));
  window.addEventListener('hashchange', route);
  wireNow();
  wireSearch();
  renderQueue();
  route();
  loadState();
  loadQueue();
  connect();
})();
