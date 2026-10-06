# MiSTer Subsonic — Plan 7 design: the web remote

Addendum to `2026-09-28-mister-subsonic-design.md` (the main spec). The main spec's §2 left a web or phone remote out of v1, and §13 lists it as a candidate. Where this document and the main spec differ, this one wins for the features below. The plan updates the main spec's §2, §8 and §13 to match.

Backlog entry: `docs/superpowers/plans/backlog.md`, section C.

## 1. Why, and what success means

The user wants to control the MiSTer from a phone or a computer on the home network (2026-10-02), for four things:
- **A couch remote:** pause, skip, volume, and seeing what's playing, without the gamepad. For example while the visualizer runs full screen.
- **Faster searching and browsing:** typing on a phone or PC keyboard instead of the on-screen keyboard.
- **Queue building:** play next, add to the end, reorder and remove, while the music keeps playing.
- **From another room:** the TV may be off.

**Success means:**
- From any browser on the home network, the user can see what's playing, control playback, edit the queue, and browse, search and play.
- The TV and every open remote always show the same state, updated live.
- It works with the TV off, and without the internet.
- With the remote off (the default), nothing listens and nothing changes.
- The server login never reaches the browser.

## 2. Access and safety

The user chose **no pairing**. With the remote on, anyone on the home network can control playback. The following protections still apply:
- **Off by default:** `[remote] enabled = false`. With it off, there is no listener at all.
- **Host header check, against DNS rebinding.** A request is accepted only if its Host is an IP literal, `localhost`, or this machine's hostname, with or without `.local`. This is the dev viewer's rule (`internal/devview`), shared.
- **Same-origin writes, against CSRF.** Every POST must have `Content-Type: application/json`, and an `Origin` header (when present) equal to the request's own host. Otherwise it gets 403.
- **No secrets and no admin:**
  - The API exposes no settings, server list, credentials, files or logs.
  - Covers and library data are fetched by the app with its own client, and only the results are returned.
- **README:** a short note says the remote is open to everyone on the home network, and that it uses plain http.
- **Logging:** each request that fails a check is logged once per minute at most, with no headers that might carry data.

## 3. Architecture

### 3.1 `internal/remote`
- **The server:** `remote.New(ctl Controller, opts Options) *Server`, with these methods:
  - `Listen(addr string) error`;
  - `Handler() http.Handler`;
  - `URLs() []string`: `http://<lan-ip>:<port>/` for each non-loopback IPv4 address;
  - `Close() error`;
  - `Notify(kind Change)`: the UI calls it when the player state or the queue changes.
- **The page's files** (`index.html`, `app.js`, `app.css`) are embedded with `embed.FS`. Plain JS, no framework, no build step, no external fonts or CDNs.
- **`Controller`** is everything the server needs from the app. Tests use fakes.

  ```go
  type Controller interface {
      State() State        // a snapshot: song, status, position, duration, volume dB, muted, shuffle, repeat, starred, queue index
      Queue() []Song       // the queue, in play order
      Do(c Command) error  // a playback or queue command, run on the UI goroutine; returns when it has run or been refused
      Play(p PlayRequest) error // resolve an album, playlist, artist or song ids to songs, then PlayNow, PlayNext or Enqueue
      Library() Library    // read-only browse and search (the app's Subsonic client)
      Cover(ctx context.Context, id string, size int) ([]byte, string, error) // image bytes and content type
  }
  ```
- `State`, `Song`, `Command` and `PlayRequest` are small JSON-friendly types in `internal/remote`. Songs carry an id, title, artist, album, duration, cover id and starred. Nothing else.

### 3.2 The UI side (`internal/ui/remote.go`)
- **`(*App).Remote() remote.Controller`** is the adapter.
- **`Do` and `Play`** post to the UI goroutine (`App.Post`) and wait for the result, with a 5 s timeout. They then run the same code as the matching button:
  - volume shows the volume panel on the TV and saves the setting;
  - mute toggles as with hold Select;
  - star uses the app's star logic and cache;
  - shuffle and repeat use the player setters, as the X menu does.
- **`State` and `Queue`** read the player's snapshot (`Player.State()` is already safe to read concurrently).
- **`Library`:** browse and search call the app's Library directly from the request goroutine. The Subsonic client is goroutine-safe, so nothing touches UI state.
- **`Cover`:**
  1. it uses the art loader's on-disk cache when the cover is there;
  2. otherwise it fetches `getCoverArt` with `size` through the app's client;
  3. it never decodes or re-encodes, so the bytes pass through.
- **`Notify`:** the app calls the server's `Notify` from its player-event handling (`onPlayer`), and when the queue changes.

### 3.3 The player
- **New:** `Player.Move(from, to int)` moves a queue entry, keeping the current song and its position. It also joins the UI's `Player` interface and the fakes.

### 3.4 Wiring and config
- **Config:**
  - `[remote] enabled = false`, `port = 8080`.
  - The editor and `config.example.toml` know the keys.
  - A port outside 1–65535 loads as 8080, with a warning.
- **`cmd/mistersubsonic`** starts the server after the UI exists, when the setting is on, binding `0.0.0.0:<port>`.
  - If the bind fails, a toast says "Remote: port 8080 is in use" and the app carries on.
  - The server stops on exit, before the player.
- **Settings → Remote,** a new Settings item after Display:
  - **Remote: On/Off**, which starts or stops the server at once and saves;
  - **an info row** with the address, for example `http://192.168.1.50:8080`. It shows "no network" when there is no LAN address.

## 4. The API

JSON over HTTP. Errors are `{"error": "message"}`, with 400, 403, 404 or 503.

### 4.1 State and live updates
- **`GET /api/state`:** `{song, status, position_ms, duration_ms, volume_db, muted, shuffle, repeat, index}`. Here `song` is null when nothing is loaded, `status` is playing, paused, loading, buffering or stopped, and `repeat` is off, all or one.
- **`GET /api/queue`:** `{index, songs: [...]}`.
- **`GET /api/events`:** Server-Sent Events.
  - `event: state` on every change, and once per second while playing (the position).
  - `event: queue` when the queue changes.
  - A comment heartbeat every 15 s.
  - The first events after connecting are the current state and queue.
  - The server holds at most 8 connections; a ninth gets 503.

### 4.2 Commands
`POST /api/cmd` with one of these:

| `do` | Fields | Effect |
|---|---|---|
| `toggle` | | play/pause |
| `next`, `prev` | | as L and R |
| `seek` | `position_ms` | within the song |
| `volume` | `db` (−60..0) | as Up and Down; the panel shows on the TV |
| `mute` | `on` | |
| `shuffle` | `on` | |
| `repeat` | `mode` (off, all, one) | |
| `star` | `on` | the current song |
| `jump` | `index` | play from that queue entry |
| `remove` | `index` | |
| `move` | `from`, `to` | |
| `clear` | | empty the queue |
| `screenshot` | | as the screenshot key (F12): a PNG in the screenshots folder, and the toast on the TV. 409 `remote: screenshot in progress` while one is saving, and also when the last one from the remote was less than 1 s ago (so a phone that taps again and again doesn't write to the SD card); 503 `remote: screenshots are off` with no screenshots folder |

- An index that is out of range, or no longer refers to the song the page showed, gets 409. The page then reloads the queue.
- To make that check possible, `jump`, `remove` and `move` carry the song id the page saw.

### 4.3 Play
- `POST /api/play` with `{what: "album"|"playlist"|"artist"|"songs", id | ids, start, how: "now"|"next"|"end"}`.
  - `artist` plays all of the artist's albums, in order.
  - `start` is the song index to begin at, for `now`.
- The response is `{added: N}`.

### 4.4 Browse and search
All of these are read-only, with a 10 s timeout.
- **Artists:** `GET /api/artists`, grouped by index letter.
- **One artist:** `GET /api/artist/{id}`, with its albums.
- **Albums:** `GET /api/albums?list=recent|random|newest&offset=N`, 50 per page.
- **One album:** `GET /api/album/{id}`, with its songs.
- **Playlists:** `GET /api/playlists` and `GET /api/playlist/{id}`.
- **Starred:** `GET /api/starred`, with artists, albums and songs.
- **Genres:** `GET /api/genres` and `GET /api/genre/{name}?offset=N`, the genre's albums.
- **Search:** `GET /api/search?q=`, giving artists, albums and songs. The minimum is 2 characters.

### 4.5 Covers and the page
- **`GET /api/cover/{id}?size=N`:**
  - N is clamped to 64–600; the default is 300;
  - `Cache-Control: max-age=86400`;
  - 404 when the server has none.
- **`GET /`, `/app.js`, `/app.css`:** the embedded files, with `Cache-Control: no-cache`, so an app update shows at once.

## 5. The page

- **Layout:**
  - **Phone:** a bottom tab bar with Now Playing, Queue, Browse and Search.
  - **Desktop width, ≥ 900 px:** Now Playing as a left column; Queue, Browse and Search as tabs on the right.
- **Now Playing:**
  - the cover (large on a phone);
  - the title, artist and album;
  - a progress bar you can tap or drag to seek;
  - previous, play/pause and next;
  - a volume slider and mute;
  - star, shuffle and repeat (off, all, one);
  - a small Screenshot button on its own line above the cover, at the right. It is a deliberate exception to the 44 px rule: it is a rare maintenance action, and a full-size button would take a line of the phone's screen from the cover.
- **Queue:**
  - the current song is highlighted, and tapping a song plays from it;
  - each row has a ⋯ menu: Remove, Move up, Move down;
  - "Clear queue" asks for confirmation.
- **Browse:**
  - Artists, A–Z with a letter jump. An artist shows its albums.
  - Albums: Recent, Random and Newest, with "More" for paging.
  - Playlists, Starred and Genres.
  - An album or playlist shows its songs, with Play, Play next and Add to queue at the top. Each song has the same three in its ⋯ menu; Play there starts from that song.
- **Search:**
  - one box, which searches 300 ms after the last keystroke;
  - the results are grouped into Artists, Albums and Songs, with the same actions.
- **Look:**
  - the TV app's dark theme and accent colour, and large touch targets of at least 44 px;
  - a toast confirms each action, for example "Added 12 songs" or "Playing next".
- **Live state:**
  - Now Playing and the Queue are redrawn from SSE events.
  - The position moves smoothly in the browser between ticks.
  - If the connection drops, a banner says "Reconnecting…", and the page retries after 1, 2, 4 … 30 s. On reconnect it reloads the state.
- **Size:** about 30–50 KB in total, uncompressed.

## 6. Edge cases

- **The queue changes under a phone:** commands carry the song id the page saw. A mismatch gets 409, and the page reloads the queue and shows "The queue changed".
- **Several remotes at once:** every event goes to all connections, so all of them, and the TV, stay in sync. The last command wins.
- **The Subsonic server is unreachable:** browse and search return 503 with the server's message. Playback commands still work for what is already queued.
- **Nothing playing:** `song` is null, the Now Playing page shows "Nothing playing" plus a Browse button, and the transport commands do nothing.
- **App exit:** the server closes its listener and its SSE connections. Open pages show "Reconnecting…".
- **Turning the remote off in Settings:** the same as exit, for the remote only.
- **The UI goroutine is busy:** `Do` waits at most 5 s, then returns 503, "The MiSTer is busy".
- **Huge libraries:** the artist list can be thousands of entries. It is sent once, grouped, and the page renders it lazily by letter.
- **CPU on the MiSTer:**
  - SSE ticks are once a second, and only while connections exist.
  - With no connections, `Notify` costs a check of the connection count.
  - The server takes its own goroutines, not the UI's.

## 7. Code

| Where | What |
|---|---|
| `internal/remote/server.go` | the server, routing, Host and Origin checks, SSE hub |
| `internal/remote/api.go` | the JSON types, and the state, commands, play, browse, search and cover handlers |
| `internal/remote/web/` | `index.html`, `app.js`, `app.css` (embedded) |
| `internal/ui/remote.go` | the `Controller` adapter: Post, wait, the same code as the buttons; `Notify` hooks |
| `internal/player/player.go` | `Move(from, to)` |
| `internal/ui/screens_settings.go` | Settings → Remote |
| `internal/config` | `[remote] enabled`, `port`, also in `config.example.toml` |
| `cmd/mistersubsonic` | starting and stopping the server, the bind-failure toast |
| `internal/devview` | the Host check, moved to a small shared helper both servers use |

## 8. Testing

- **`internal/remote`**, with fake Controller and Library implementations and `httptest`:
  - every endpoint's JSON;
  - Host rejection, covering a DNS name that isn't the machine's and an IP literal accepted;
  - POST without a JSON content type, or with a foreign Origin, gets 403;
  - a stale index gets 409;
  - SSE: the initial state and queue, an event after `Notify`, the heartbeat, the connection cap, and a clean close;
  - covers: size clamping and the cache headers;
  - the embedded page is served with no-cache.
- **`internal/ui`:** with the real App, `Do` and `Play` change the player exactly as the buttons do (volume panel shown and saved, star cache, queue Move), and `Notify` fires on player events and queue changes.
- **`internal/player`:** `Move` keeps the current song playing, and its index follows the move.
- **`internal/config`:** the keys, the default, the bad-port warning, and an editor round trip.
- **`cmd/mistersubsonic`:** the server starts when enabled, the bind failure is a toast not a crash, and it closes on exit.
- **e2e (`scripts/e2e-ui.sh`):** the app runs with the mock Subsonic server and the remote on; curl fetches `/api/state`, sends a toggle, and runs a search and a play. It also posts `screenshot` and checks that a PNG appears in the screenshots folder.
- **JS:** if `node` is on the build machine, a small test of the page's state reducer runs. Otherwise it is skipped. The page itself is checked by hand on the desktop app (`-display view`, with the remote on) before the plan's last task.
- **No test touches real devices or sound.**

## 9. On the MiSTer (when it is back)

- Run the remote check with the user's go-ahead:
  - open the address from Settings on a phone;
  - control playback with the TV on, then with the TV off;
  - edit the queue while music plays;
  - search and play an album.
- Measure the CPU with a phone connected (`top`) during playback: the SSE ticks and requests must stay under 5% of one core.
- Ask each time before anything plays.

## 10. Docs

- **README:**
  - how to turn the remote on, and where the address is;
  - what it can do;
  - the safety note (§2).
- **`docs/testing-on-mister.md`:** new items for §9.
- **The main spec:** §2 (the remote is no longer a non-goal), §8 (Settings → Remote), and §13.
- **The backlog:** C is done, except the device check.

## 11. Out of scope

- Pairing and accounts; https.
- Settings, server switching and the visualizer's controls from the remote.
- Lyrics, playlist editing, and creating playlists.
- An installable app (PWA), notifications, and media-session lock-screen controls.
