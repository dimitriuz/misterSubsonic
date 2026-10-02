'use strict';
// Run with: node --test app_test.js
const test = require('node:test');
const assert = require('node:assert');
const A = require('./app.js');

const song = (id) => ({ id, title: 't' + id, artist: 'a', album: 'b', cover_id: 'c' + id, duration_ms: 200000, starred: false });
const st = (o) => Object.assign({ song: song('s1'), status: 'playing', position_ms: 10000, duration_ms: 200000,
  volume_db: -10, muted: false, shuffle: false, repeat: 'off', index: 0 }, o);

test('reduce: state event stores the state and when it came', () => {
  const s0 = A.initial();
  assert.strictEqual(s0.state, null);
  const s1 = A.reduce(s0, { type: 'state', data: st(), now: 5000 });
  assert.strictEqual(s1.state.status, 'playing');
  assert.strictEqual(s1.at, 5000);
  assert.strictEqual(s0.state, null, 'the old state is not changed');
});

test('reduce: queue event, and a state with a new index follows the queue index', () => {
  let s = A.reduce(A.initial(), { type: 'queue', data: { index: 1, songs: [song('a'), song('b')] } });
  assert.strictEqual(s.queue.songs.length, 2);
  assert.strictEqual(s.queue.index, 1);
  s = A.reduce(s, { type: 'state', data: st({ index: 0 }), now: 1 });
  assert.strictEqual(s.queue.index, 0, 'the highlighted row moves with the state');
});

test('reduce: a null songs list becomes empty', () => {
  const s = A.reduce(A.initial(), { type: 'queue', data: { index: 0, songs: null } });
  assert.deepStrictEqual(s.queue.songs, []);
});

test('reduce: connection events', () => {
  let s = A.reduce(A.initial(), { type: 'conn', up: true });
  assert.strictEqual(s.connected, true);
  s = A.reduce(s, { type: 'conn', up: false });
  assert.strictEqual(s.connected, false);
});

test('reduce: unknown events change nothing', () => {
  const s = A.initial();
  assert.strictEqual(A.reduce(s, { type: 'bogus' }), s);
});

test('position: moves while playing, clamps to the duration', () => {
  const s = A.reduce(A.initial(), { type: 'state', data: st(), now: 1000 });
  assert.strictEqual(A.position(s, 1000), 10000);
  assert.strictEqual(A.position(s, 3500), 12500);
  assert.strictEqual(A.position(s, 1e9), 200000);
  assert.strictEqual(A.position(s, 500), 10000, 'never goes back before the tick');
});

test('position: stands still unless playing', () => {
  for (const status of ['paused', 'loading', 'buffering', 'stopped']) {
    const s = A.reduce(A.initial(), { type: 'state', data: st({ status }), now: 1000 });
    assert.strictEqual(A.position(s, 9000), 10000, status);
  }
  assert.strictEqual(A.position(A.initial(), 9000), 0);
});

test('position: no duration means no clamp', () => {
  const s = A.reduce(A.initial(), { type: 'state', data: st({ duration_ms: 0 }), now: 0 });
  assert.strictEqual(A.position(s, 4000), 14000);
});

test('backoff: 1, 2, 4 ... capped at 30 s', () => {
  const got = [0, 1, 2, 3, 4, 5, 6, 20].map(A.backoff);
  assert.deepStrictEqual(got, [1000, 2000, 4000, 8000, 16000, 30000, 30000, 30000]);
});

test('formatTime', () => {
  assert.strictEqual(A.fmtTime(0), '0:00');
  assert.strictEqual(A.fmtTime(65400), '1:05');
  assert.strictEqual(A.fmtTime(3600000 + 61000), '1:01:01');
  assert.strictEqual(A.fmtTime(-5), '0:00');
  assert.strictEqual(A.fmtTime(NaN), '0:00');
});

const reply = (status, body) => async () => ({ ok: status >= 200 && status < 300, status, json: async () => body });

test('api: get returns the JSON and calls the right URL', async () => {
  let seen;
  const api = A.makeApi(async (url, opt) => { seen = [url, opt]; return reply(200, { a: 1 })(); });
  assert.deepStrictEqual(await api.get('/api/state'), { a: 1 });
  assert.strictEqual(seen[0], '/api/state');
});

test('api: post sends JSON with the JSON content type', async () => {
  let seen;
  const api = A.makeApi(async (url, opt) => { seen = [url, opt]; return reply(200, { ok: true })(); });
  await api.post('/api/cmd', { do: 'toggle' });
  assert.strictEqual(seen[1].method, 'POST');
  assert.strictEqual(seen[1].headers['Content-Type'], 'application/json');
  assert.strictEqual(seen[1].body, '{"do":"toggle"}');
});

test('api: errors carry the status and the server message', async () => {
  const api = A.makeApi(reply(409, { error: 'remote: the queue changed' }));
  await assert.rejects(api.post('/api/cmd', {}), (e) => e.status === 409 && e.stale === true && /queue changed/.test(e.message));
  const api2 = A.makeApi(reply(503, { error: 'server unreachable' }));
  await assert.rejects(api2.get('/api/artists'), (e) => e.status === 503 && !e.stale && e.message === 'server unreachable');
});

test('api: a failed fetch or a non-JSON error body is an error with status 0 or the code', async () => {
  const api = A.makeApi(async () => { throw new TypeError('network'); });
  await assert.rejects(api.get('/x'), (e) => e.status === 0);
  const api2 = A.makeApi(async () => ({ ok: false, status: 500, json: async () => { throw new Error('bad json'); } }));
  await assert.rejects(api2.get('/x'), (e) => e.status === 500 && e.message.length > 0);
});

test('plural and added messages', () => {
  assert.strictEqual(A.addedMessage('end', 12), 'Added 12 songs');
  assert.strictEqual(A.addedMessage('end', 1), 'Added 1 song');
  assert.strictEqual(A.addedMessage('next', 3), 'Playing next');
  assert.strictEqual(A.addedMessage('now', 3), 'Playing');
});

test('plural', () => {
  assert.strictEqual(A.plural(1, 'album'), '1 album');
  assert.strictEqual(A.plural(0, 'album'), '0 albums');
  assert.strictEqual(A.plural(2, 'song'), '2 songs');
});

test('queue move: the target index and its bounds', () => {
  assert.deepStrictEqual(A.moveTarget(2, 5, -1), { from: 2, to: 1 });
  assert.deepStrictEqual(A.moveTarget(2, 5, 1), { from: 2, to: 3 });
  assert.strictEqual(A.moveTarget(0, 5, -1), null);
  assert.strictEqual(A.moveTarget(4, 5, 1), null);
});

test('route: hash to tab and browse path', () => {
  assert.deepStrictEqual(A.parseHash(''), { tab: 'now', rest: '' });
  assert.deepStrictEqual(A.parseHash('#queue'), { tab: 'queue', rest: '' });
  assert.deepStrictEqual(A.parseHash('#browse/album/al-1'), { tab: 'browse', rest: 'album/al-1' });
  assert.deepStrictEqual(A.parseHash('#search'), { tab: 'search', rest: '' });
  assert.deepStrictEqual(A.parseHash('#nonsense'), { tab: 'now', rest: '' });
});
