// Command mkdb writes the MiSTer Downloader database for a release: every
// file under -dir (the SD card tree: Scripts/, mistersubsonic/) with its
// MD5 and size, downloaded from -base-url plus the file's name (the
// release's assets). Users add the database to downloader.ini, and
// Downloader or update_all then install and update the app.
//
//	go run ./tools/mkdb -dir bin/release/sdcard -base-url https://github.com/OWNER/REPO/releases/download/v1.0.0/ -o db.json
package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"time"
)

// DB is Downloader's database format (docs/custom-databases.md in
// MiSTer-devel/Downloader_MiSTer).
type DB struct {
	V         int                 `json:"v"`
	ID        string              `json:"db_id"`
	Timestamp int64               `json:"timestamp"`
	Files     map[string]File     `json:"files"`
	Folders   map[string]struct{} `json:"folders"`
}

// File is one file of the database.
type File struct {
	Hash string `json:"hash"` // MD5, hex
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

// build walks dir and describes every file in it. Each file is downloaded
// from baseURL + its name, so names must be unique across folders.
func build(dir, id, baseURL string, ts int64) (*DB, error) {
	db := &DB{V: 1, ID: id, Timestamp: ts, Files: map[string]File{}, Folders: map[string]struct{}{}}
	names := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		name := path.Base(rel)
		if other, dup := names[name]; dup {
			return fmt.Errorf("%s and %s share the download name %q", other, rel, name)
		}
		names[name] = rel
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := md5.Sum(b)
		db.Files[rel] = File{Hash: hex.EncodeToString(sum[:]), Size: int64(len(b)), URL: baseURL + name}
		for f := path.Dir(rel); f != "."; f = path.Dir(f) {
			db.Folders[f] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(db.Files) == 0 {
		return nil, errors.New("no files in " + dir)
	}
	return db, nil
}

func main() {
	dir := flag.String("dir", "bin/release/sdcard", "the SD card tree to describe")
	id := flag.String("id", "mistersubsonic", "db_id: the section name users put in downloader.ini")
	baseURL := flag.String("base-url", "", "where the files are downloaded from (ends in /)")
	out := flag.String("o", "db.json", "output file")
	ts := flag.Int64("timestamp", 0, "the database timestamp (default: now)")
	flag.Parse()
	if *baseURL == "" {
		fmt.Fprintln(os.Stderr, "mkdb: -base-url is required")
		os.Exit(2)
	}
	if *ts == 0 {
		*ts = time.Now().Unix()
	}
	db, err := build(*dir, *id, *baseURL, *ts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mkdb:", err)
		os.Exit(1)
	}
	b, _ := json.MarshalIndent(db, "", "  ")
	if err := os.WriteFile(*out, append(b, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "mkdb:", err)
		os.Exit(1)
	}
}
