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
9. **Mute.** Press M on a keyboard, and hold Select on Now Playing for a second (a short
   Select press still cycles shuffle/repeat, and the release after the hold does
   nothing). The volume keys bring the sound back. Settings → Playback has no
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
21. **Screenshots.** Press Print Screen, then use the MiSTer Companion
    remote's Capture screenshot button. Each shows "Screenshot saved", adds a
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
    Select has no key, so a keyboard shows no Select hint; the screensaver
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
    - Let the screensaver start while a track plays, then stop the server or unplug the network: the error's toast wakes the screen.
    - A key that wakes the screensaver does nothing else. On Now Playing, a Select press that wakes it neither mutes nor changes the mode.
27. **MP3 seeking.** In a long MP3 album, seek a little forward and back with Left and Right on Now Playing: the next track stays queued (no gap at the end of the track). A VBR MP3 lands near the time shown.
28. **Config comments.** Add a comment line to `config.toml`, change a setting in the app, and exit: the comment is still there.
29. **Launcher signals.** Over ssh, run `pkill -TERM -f Scripts/MiSTer_Subsonic.sh` while the app runs: the app exits, and the screen and menu come back as after a normal exit.
30. **Log.** `/media/fat/mistersubsonic/log.txt` has a "starting" and an
    "exiting" line for each run, and `crash.txt` beside it is empty.

## Benchmarks

`make deploy-dev MISTER=<ip>` copies the test binaries. The spikes and the
render benchmark (`ui.test -test.bench Repaint`) are described in
`docs/spikes.md`; record their results there.
