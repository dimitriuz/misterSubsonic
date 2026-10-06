# Testing on the MiSTer

A manual checklist for a real MiSTer (spec §10). Run it before a release and
after changes to the launcher, the console handling, input or the display.
Record the results, with the date and the app version (Settings → About), in
`docs/spikes.md`.

**Sound:** turn the TV or amplifier down before the first start. The app
starts at the config's `volume_db` (0 dB by default) on the MiSTer.

## Install

- From this repository: `make deploy MISTER=<ip>` builds the release and
  copies it over ssh (the default root password is `1`).
- From a release: unzip `MiSTer_Subsonic-<version>.zip` onto the SD card root.

## Checklist

1. **Start.** Scripts → MiSTer_Subsonic. The app fills the screen, with no
   console text or cursor over it.
2. **Setup wizard.** With no `config.toml`, the wizard starts. Enter a server
   with the controller, then again with a USB keyboard. Afterwards
   `config.toml` has `token` and `salt` and no `password`.
3. **HDMI.** At 720p, 1080p and a mode above 1080p such as 1920x1200
   (`video_mode`, see the README): the image fills the screen and the text is
   sharp. Covers load in the grids. Scrolling a long list keeps up with a held
   D-pad, at 1080p and 1920x1200 too.
4. **CRT.** At 240p and 288p: the CRT layout, with nothing important cut off
   by overscan. Tune the title-safe margins if needed.
5. **Controllers.** The user's MiSTer mapping works. Unplug the pad while the
   app runs and plug it back in: it works again within about 2 s.
6. **Long FLAC.** Play a single-file album rip over 1 GB and seek to about
   80%. Playback resumes within about a second.
7. **Gapless.** Play a gapless album (a live album or a mix): no gap between
   tracks.
8. **Network drop.** Unplug the network or turn off Wi-Fi mid-track. The app
   shows buffering, then plays on when the network is back, or shows a clear
   error.
9. **Mute.** Press M on a keyboard, and hold Select (V on a keyboard) on Now Playing for a second (a short
   Select press changes the visualizer style on Now Playing, because shuffle
   and repeat are in the X menu; the release after the hold does nothing). The volume keys bring the sound back. Settings → Playback has no
   Volume or Mute rows.
10. **Exit.** Use Exit in the main menu (the sidebar's last entry on HDMI, the home list's last item on CRT), and holding B for 2 s on the home screen.
    Both return to the Scripts menu with the text readable and "MiSTer
    Subsonic closed." shown. The controller works in the menu afterwards.
11. **BGM.** With BGM playing in the menu, start the app: the music stops.
    Exit: BGM plays again.
12. **SAM.** With Super Attract Mode enabled, leave the app idle past SAM's
    timeout: no game starts. After exit, SAM is enabled again.
13. **Crash recovery.** Over ssh, run `kill -9 $(pidof mistersubsonic)` while
    the app runs. The launcher shows "stopped with an error" (it's `kill -9`),
    puts the console back and returns to the menu, and the next start works.
14. **Leftover app.** Start the app over ssh
    (`/media/fat/mistersubsonic/mistersubsonic &`), then from the Scripts
    menu. The first one is stopped and the new one takes over.
15. **Overwrite watchdog.** Over ssh, run
    `head -c 200000 /dev/urandom > /dev/fb0` while the app shows a still
    screen. The noise is gone within about 2 s.
16. **Long MP3.** Seek to about 80% of a long constant-bitrate MP3 (a
    podcast or a mix). Playback resumes within about a second, near the
    target.
17. **ReplayGain.** With a quiet and a loud album, switch Settings → Playback
    → ReplayGain between off and track while a song plays: the level changes
    within about half a second.
18. **Memory.** During a gapless album, check
    `grep VmRSS /proc/$(pidof mistersubsonic)/status` over ssh after each of
    the first five track changes, and record the values. They level off
    rather than keep growing.
19. **Media keys.** On a multimedia keyboard (for example a Logitech K400
    Plus), on a browse screen and on Now Playing: volume up and down (held
    too), mute, play/pause, next, previous, fast-forward and rewind (held too).
    Each works without leaving the screen.
20. **Volume panel.** Every volume or mute change shows the panel for about
    1.5 s: speaker, bar and level, or "Muted".
21. **Screenshots.** Press F12 on the keyboard (Print Screen and Scroll Lock
    work too), then use the MiSTer Companion remote's Capture screenshot
    button, then the Screenshot button on the web remote's Now Playing tab.
    Each shows "Screenshot saved", adds a
    PNG to `/media/fat/screenshots/MiSTer_Subsonic/`, and the Companion shows
    the picture.
22. **Full resolution.** With `video_mode=1920,1200,60`: `log.txt` says
    "framebuffer 1920x1200, full resolution (was 960x600)", and the text is as
    sharp as MiSTerHiFi's. After exit, and after `kill -9 $(pidof
    mistersubsonic)` over ssh, `cat /sys/module/MiSTer_fb/parameters/mode`
    shows 960 600 again and the menu and other scripts look as before. The
    launcher's closing message reads correctly. While the app runs, unplug and
    replug the display (or power-cycle the TV): the MiSTer menu takes the screen
    back, and within about 2 s the app quits (the music stops) with the
    launcher's closing message; no scrambled picture stays and starting the app
    again gives full resolution. `log.txt` says the display was reconnected and
    has no false "didn't switch" error at exit.
23. **Smooth browsing.** Hold the D-pad in a cover grid and in a long list
    (Artists): the focus keeps up. The progress bar and a scrolling title move
    smoothly.
24. **Hints.** Every screen shows a hint bar. It shows A/B/X/Y after a
    gamepad press and Enter/Esc/Tab/N after a key press. Settings → Display →
    Hints → Off removes it. Exceptions: the X menu shows only Choose and Close;
    Now Playing isn't hinted while typing on a keyboard (N types there);
    a keyboard shows Select hints with the cap V; the screensaver
    hides the bar; a screen that failed to load hints Retry (A).
25. **No stale pixels.** Exit the app, then over ssh run
    `/media/fat/mistersubsonic/mistersubsonic -verify-redraw` (the launcher
    can't pass flags). If the app doesn't appear on the TV, open the Scripts
    menu's console first. Play music while you browse, so the progress bar and
    the mini bar are checked too. Browsing is slower in this mode, because
    every partial frame is checked against a full one. After a minute, exit:
    `log.txt` has no "partial redraw differs" line.
26. **Settings and the screensaver.**
    - Hold Right on Settings → Playback → Scrobbling: it flips once.
    - Let the screensaver start near the end of a track, so the next track needs a fetch, then stop the server or unplug the network: the error's toast wakes the screen. It can take up to about 30 s (the retry budget).
    - A key that wakes the screensaver does nothing else. On Now Playing, a Select (V) press that wakes it neither mutes nor changes the mode.
27. **MP3 seeking.** In a long MP3 album, seek a little forward and back with Left and Right on Now Playing. In the last 20 s of a track (when the next one is queued), tap Left once: it plays on from there and the next track starts without a gap. A VBR MP3 lands near the time shown.
28. **Config comments.** Add a comment line to `config.toml`, change a setting in the app, and exit: the comment is still there.
29. **Launcher signals.** Start the launcher in one ssh session with `/media/fat/Scripts/MiSTer_Subsonic.sh; echo $?`. From a second ssh session, run `kill -TERM $(ps | grep '[S]cripts/MiSTer_Subsonic.sh' | awk '{print $1}')`. Expected: the app exits, the screen and menu come back, and the first session prints "MiSTer Subsonic closed." and `0`.
30. **Visualizer benchmarks.** On the MiSTer, in the folder `deploy-dev` copied to, run `./ui.test -test.run '^$' -test.bench VizFrame -test.benchtime 30x`, `./ui.test -test.run '^$' -test.bench VizRealFrame -test.benchtime 50x` and `./viz.test -test.run '^$' -test.bench AnalyzerUpdate`. Record the numbers in `docs/spikes.md` ("Plan 6 on the MiSTer"). No sound or picture is involved.
31. **Visualizer listening check.** Ask the user before playing anything, and keep the volume low. Play a FLAC album for a few minutes with each style (Bars, Scope, VU, Waterfall) in full screen (Start on Now Playing). There are no dropouts, and `log.txt` has no "visualizer: slowing to" line. If it does, set the frame-rate defaults so that each frame fits in 60% of its budget.
32. **Visualizer on the TV.** Ask the user before showing anything. The panel under the track info is in a sensible place on HDMI and on a CRT (inside the title-safe area, and on a CRT the text block moves up about 10 px when the visualizer is on, by design). The motion is in time with the sound: bass hits land with the beat, and after a seek or a skip the picture follows within a moment.
33. **Full screen and the corner line.** After a minute in full screen the title and time line moves to the next corner (top left, top right, bottom right, bottom left). The info line is the part that moves; VU (the L/R glyphs and the segment grid) and Scope (its midline) have a fixed frame by design. The hint bar shows after a press and hides after 3 s. Select (V on a keyboard) in full screen cycles the styles without Off, and Start or B leaves.
34. **Visualizer and the screensaver.** In full screen, while music plays, loads or buffers, the screensaver does not start. With the panel on Now Playing the screensaver works as usual. Pause in full screen and wait: it starts after its idle time over the full screen, and a key wakes it without doing anything else.
35. **Web remote, TV on and off.** Ask the user before playing anything, and keep the volume low. Turn Settings → Remote on and open the address it shows on a phone. Play, pause, skip, seek, change the volume, mute, star and switch shuffle and repeat from the page: the TV follows within a moment, and the page follows the gamepad. Then turn the TV off (the music goes on) and do the same from the phone. A bad port in `config.toml` loads as 8080, with a warning. Only a port that is already in use leaves Remote off, with a toast.
36. **Remote queue editing.** Ask the user before playing anything. While music plays, from the phone: tap a queue row to play from it, move a song up and down (the current song keeps playing, and the TV queue matches), remove one, and clear the queue. Do the same on the TV at the same time: a stale tap on the phone shows "The queue changed" and reloads.
37. **Remote search and play.** Ask the user before playing anything. On the phone, search for an artist, an album and a song, then play an album, play a song next and add one to the end. Covers show. The Queue tab and the TV agree. Pull the network cable or turn off the phone's wifi for a moment: the page shows "Reconnecting…" and recovers by itself.
38. **Remote CPU.** Ask the user before playing anything. With a phone connected (Now Playing open) and music playing, run `top` over ssh for a minute. The phone adds less than 5% of one core to the app's CPU compared with the same playback before it connected (record both numbers in `docs/spikes.md`, "Plan 7 on the MiSTer").
39. **Plan 7b checks.** Ask the user before playing anything, and keep the volume low.
    - With a long queue playing (thousands of songs), `servers/<name>-<id>/state.json`'s mtime stays put for minutes (`ls -l --time-style=full-iso`), while `position.json` updates every 30 s.
    - `log.txt` has no "savePlayQueue" error line.
    - The per-server folder is `servers/<name>-<8 hex digits>/` (or the legacy `servers/<name>/` on an upgraded install), and the cover art and scrobble cache is its `cache/` folder, not `/media/fat/mistersubsonic/cache/`.
    - Restart the app and accept the Resume card: the whole local queue is back, not the server's 500-song window.
    - The visualizer in full screen at 1920×1200 holds its rate, and `log.txt` has no "visualizer: slowing to" line.
    - The waterfall looks smooth: no vertical bands, and no steps between one frame and the next.
    - V cycles the visualizer style on a keyboard (and a text field takes V as a letter), and F12 saves a screenshot.
    - The web remote's Screenshot button saves a PNG too ("Screenshot saved" on the phone and the TV). Tapping it again within a second shows "screenshot in progress".
40. **Log.** `/media/fat/mistersubsonic/log.txt` has a "starting" and an
    "exiting" line for each run, and `crash.txt` beside it is empty.

## Benchmarks

`make deploy-dev MISTER=<ip>` copies the test binaries. The spikes and the
render benchmark (`ui.test -test.bench Repaint`) are described in
`docs/spikes.md`; record their results there.
