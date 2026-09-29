// Command mss-cli is a terminal harness for the playback core: it connects
// to the configured server and plays albums with line-based controls.
// It is a development and on-device test tool, not the app.
//
//	mss-cli -config config.toml ping
//	mss-cli -config config.toml albums
//	mss-cli -config config.toml play-album <album-id>
//	mss-cli -config config.toml play-song <song-id>...
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"mistersubsonic/internal/audio"
	"mistersubsonic/internal/config"
	"mistersubsonic/internal/player"
	"mistersubsonic/internal/subsonic"
)

func main() {
	cfgPath := flag.String("config", config.DefaultPath, "config file")
	null := flag.Bool("null", false, "use the null audio device (no sound; for testing)")
	quitAtEnd := flag.Bool("exit-at-end", false, "exit when the queue finishes")
	volume := flag.Float64("volume", math.NaN(), "start volume in dB (-60..0), overriding the config; use -30 for a quiet first listen")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: mss-cli [flags] ping | albums | search <query> | play-album <id> | play-song <id>...")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*cfgPath, *null, *quitAtEnd, *volume, flag.Args()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(cfgPath string, null, quitAtEnd bool, volume float64, args []string) error {
	cfg, warns, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	for _, w := range warns {
		log.Printf("config: %s", w)
	}
	if !math.IsNaN(volume) {
		cfg.Playback.VolumeDB = volume
	}
	srv, ok := cfg.ActiveServer()
	if !ok {
		return errors.New("no [[server]] in config")
	}
	client, err := subsonic.New(subsonic.Options{
		BaseURL: srv.URL,
		Credentials: subsonic.Credentials{Username: srv.Username, Password: srv.Password, Token: srv.Token, Salt: srv.Salt,
			APIKey: srv.APIKey, AllowPlaintext: srv.AllowPlaintextPassword},
		CAFile: srv.CAFile, InsecureSkipVerify: srv.InsecureSkipVerify,
	})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	info, err := client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("%v: %w", subsonic.Classify(err), err)
	}

	switch args[0] {
	case "ping":
		fmt.Printf("ok: %s %s, API %s, OpenSubsonic=%v, auth=%v, extensions=%v\n",
			info.Type, info.ServerVersion, info.APIVersion, info.OpenSubsonic, client.AuthMethod(), info.Extensions)
		return nil
	case "albums":
		list, err := client.GetAlbumList2(ctx, subsonic.AlbumListQuery{Type: subsonic.ListNewest, Size: 50})
		if err != nil {
			return err
		}
		for _, a := range list {
			fmt.Printf("%-40s %s — %s (%d)\n", a.ID, a.Artist, a.Name, a.Year)
		}
		return nil
	case "search":
		res, err := client.Search3(ctx, strings.Join(args[1:], " "), subsonic.SearchQuery{AlbumCount: 20, SongCount: 20})
		if err != nil {
			return err
		}
		for _, a := range res.Albums {
			fmt.Printf("album %-36s %s — %s\n", a.ID, a.Artist, a.Name)
		}
		for _, s := range res.Songs {
			fmt.Printf("song  %-36s %s — %s [%s]\n", s.ID, s.Artist, s.Title, s.Suffix)
		}
		return nil
	case "play-album", "play-song":
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}

	var songs []subsonic.Song
	if args[0] == "play-album" {
		if len(args) != 2 {
			return errors.New("play-album needs one album id")
		}
		al, err := client.GetAlbum(ctx, subsonic.ID(args[1]))
		if err != nil {
			return err
		}
		songs = al.Songs
	} else {
		for _, id := range args[1:] {
			songs = append(songs, subsonic.Song{ID: subsonic.ID(id), Suffix: "flac"})
		}
	}
	if len(songs) == 0 {
		return errors.New("nothing to play")
	}

	dev := cfg.Playback.ALSADevice
	if dev == "default" {
		dev = ""
	}
	out, err := audio.OpenDevice(audio.DeviceOptions{Name: dev, Null: null})
	if err != nil {
		return err
	}
	defer out.Close()
	eng := audio.NewEngine(audio.EngineOptions{Output: out})
	defer eng.Close()

	p := player.New(player.Options{
		Engine: eng, API: client,
		Open: player.NewOpener(client, player.StreamSettings{
			TranscodeFormat: cfg.Playback.TranscodeFormat, TranscodeBitrate: cfg.Playback.TranscodeBitrate,
			WindowBytes: int64(cfg.Playback.BufferMB) << 20,
		}),
		ReplayGain: cfg.Playback.ReplayGain, Scrobble: cfg.Playback.Scrobble, VolumeDB: cfg.Playback.VolumeDB,
	})
	pctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { p.Run(pctx); close(done) }()
	defer func() { cancel(); <-done }()

	p.PlayNow(songs, 0)
	fmt.Println("controls: p pause · n next · b prev · s <sec> seek · r repeat · z shuffle · + / - volume · q quit")

	lines := make(chan string)
	go func() {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			lines <- strings.TrimSpace(sc.Text())
		}
	}()
	status := time.NewTicker(time.Second)
	defer status.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-p.Events():
			switch ev.Kind {
			case player.Error:
				fmt.Printf("\n! %s: %v\n", ev.Song.Title, ev.Err)
			case player.TrackChanged:
				fmt.Printf("\n▶ %s — %s [%s]\n", ev.Song.Artist, ev.Song.Title, quality(ev.Song))
			case player.StatusChanged:
				if quitAtEnd && p.State().Status == player.Stopped {
					fmt.Println("\nqueue finished")
					return nil
				}
			}
		case <-status.C:
			st := p.State()
			cur, _ := st.Current()
			fmt.Printf("\r  %-9s %s / %s  vol %.0f dB   ", st.Status, clock(st.Position), clock(time.Duration(cur.Duration)*time.Second), st.VolumeDB)
		case l := <-lines:
			st := p.State()
			switch {
			case l == "q":
				return nil
			case l == "p":
				p.TogglePause()
			case l == "n":
				p.Next()
			case l == "b":
				p.Prev()
			case l == "r":
				p.SetRepeat((st.Repeat + 1) % 3)
				fmt.Printf("\nrepeat %d\n", (st.Repeat+1)%3)
			case l == "z":
				p.SetShuffle(!st.Shuffle)
			case l == "+":
				p.SetVolumeDB(st.VolumeDB + 3)
			case l == "-":
				p.SetVolumeDB(st.VolumeDB - 3)
			case strings.HasPrefix(l, "s "):
				if sec, err := strconv.Atoi(strings.TrimSpace(l[2:])); err == nil {
					p.Seek(time.Duration(sec) * time.Second)
				}
			}
		}
	}
}

func quality(s subsonic.Song) string {
	q := strings.ToUpper(s.Suffix)
	if s.BitDepth > 0 && s.SamplingRate > 0 {
		q += fmt.Sprintf(" %d/%g", s.BitDepth, float64(s.SamplingRate)/1000)
	} else if s.BitRate > 0 {
		q += fmt.Sprintf(" %d", s.BitRate)
	}
	return q
}

func clock(d time.Duration) string {
	s := int(d / time.Second)
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}
