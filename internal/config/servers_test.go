package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCloneIsDeep(t *testing.T) {
	c := Default()
	c.Servers = []Server{{Name: "home", URL: "http://h:4533", Username: "a", Password: "p"}}
	d := c.Clone()
	d.Servers[0].Name = "changed"
	d.Playback.VolumeDB = -10
	if c.Servers[0].Name != "home" || c.Playback.VolumeDB != 0 {
		t.Fatal("Clone shares state with the original")
	}
}

func TestAddServerMakesNamesUniqueAndDefault(t *testing.T) {
	c := Default()
	if got := c.AddServer(Server{Name: "music.example.com", URL: "https://music.example.com", Username: "a", Password: "p"}); got != "music.example.com" {
		t.Fatalf("first name %q", got)
	}
	if got := c.AddServer(Server{Name: "music.example.com", URL: "https://music.example.com", Username: "b", Password: "p"}); got != "music.example.com-2" {
		t.Fatalf("second name %q", got)
	}
	if c.DefaultServer != "music.example.com-2" || len(c.Servers) != 2 {
		t.Fatalf("default %q, %d servers", c.DefaultServer, len(c.Servers))
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveServerMovesTheDefault(t *testing.T) {
	c := Default()
	c.AddServer(Server{Name: "a", URL: "http://a", Username: "u", Password: "p"})
	c.AddServer(Server{Name: "b", URL: "http://b", Username: "u", Password: "p"})
	if !c.RemoveServer("b") || c.DefaultServer != "a" || len(c.Servers) != 1 {
		t.Fatalf("after removing the default: %+v", c)
	}
	if c.RemoveServer("zzz") {
		t.Fatal("removed a server that isn't there")
	}
	c.RemoveServer("a")
	if c.DefaultServer != "" || len(c.Servers) != 0 {
		t.Fatalf("after removing the last: %+v", c)
	}
}

func TestSaveHeaderPointsAtTheREADME(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	c := Default()
	c.AddServer(Server{Name: "home", URL: "http://h:4533", Username: "a", Password: "p"})
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(b), "# MiSTer Subsonic configuration") || strings.Contains(string(b), "config.example.toml") {
		t.Fatalf("header: %q", strings.SplitN(string(b), "\n", 2)[0])
	}
}

func TestSaveRenameFailureIsAPathError(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.Mkdir(p, 0o755); err != nil { // a directory in the way: the rename fails
		t.Fatal(err)
	}
	c := Default()
	c.AddServer(Server{Name: "home", URL: "http://h:4533", Username: "a", Password: "secretpw"})
	err := Save(p, c)
	var pe *fs.PathError
	if err == nil || !errors.As(err, &pe) {
		t.Fatalf("err %v (%T), want a *fs.PathError", err, err)
	}
	if strings.Contains(err.Error(), "secretpw") {
		t.Fatalf("error leaks the password: %v", err)
	}
	if _, serr := os.Stat(p + ".tmp"); serr == nil {
		t.Fatal("temp file left behind")
	}
}
