package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for p, body := range files {
		full := filepath.Join(dir, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestBuildDescribesEveryFile(t *testing.T) {
	dir := tree(t, map[string]string{
		"Scripts/MiSTer_Subsonic.sh":    "#!/bin/bash\n",
		"mistersubsonic/mistersubsonic": "ELF",
	})
	db, err := build(dir, "mistersubsonic", "https://example.com/v1/", 1790000000)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(db)
	// The hashes are md5sum of the file bodies.
	want := `{"v":1,"db_id":"mistersubsonic","timestamp":1790000000,"files":{` +
		`"Scripts/MiSTer_Subsonic.sh":{"hash":"677da3bdd8fbd16d4b8917a9fe0f6f89","size":12,"url":"https://example.com/v1/MiSTer_Subsonic.sh"},` +
		`"mistersubsonic/mistersubsonic":{"hash":"b61b2d6c6fa62903b882aaa53452c111","size":3,"url":"https://example.com/v1/mistersubsonic"}},` +
		`"folders":{"Scripts":{},"mistersubsonic":{}}}`
	if string(b) != want {
		t.Fatalf("got\n%s\nwant\n%s", b, want)
	}
}

func TestBuildRefusesTwoFilesWithOneDownloadName(t *testing.T) {
	dir := tree(t, map[string]string{"a/LICENSE": "x", "b/LICENSE": "y"})
	if _, err := build(dir, "id", "u/", 1); err == nil || !strings.Contains(err.Error(), "LICENSE") {
		t.Fatalf("err %v", err)
	}
}

func TestBuildNeedsFiles(t *testing.T) {
	if _, err := build(t.TempDir(), "id", "u/", 1); err == nil {
		t.Fatal("an empty tree made a database")
	}
	if _, err := build(filepath.Join(t.TempDir(), "missing"), "id", "u/", 1); err == nil {
		t.Fatal("a missing tree made a database")
	}
}

func TestBuildListsNestedFolders(t *testing.T) {
	dir := tree(t, map[string]string{"mistersubsonic/fonts/x.ttf": "f"})
	db, err := build(dir, "id", "u/", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := db.Folders["mistersubsonic"]; !ok || len(db.Folders) != 2 {
		t.Fatalf("folders %v", db.Folders)
	}
}
