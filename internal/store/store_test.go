package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"yt-chonk/internal/core"
)

func TestExportPreservesExistingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fichier avec espaces.txt")
	if err := os.WriteFile(p, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	tr := core.Transcript{Text: "nouveau"}
	if err := Export(tr, "txt", p, false); !errors.Is(err, os.ErrExist) {
		t.Fatalf("expected collision: %v", err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != "original" {
		t.Fatal("existing export overwritten")
	}
	if err := Export(tr, "txt", p, true); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(p)
	if string(b) != "nouveau" {
		t.Fatal("force did not replace")
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatal("temporary file leaked")
	}
}
func TestExportsShareTextAndNullMetadata(t *testing.T) {
	dir := t.TempDir()
	tr := core.Transcript{SchemaVersion: 1, Video: core.Video{ID: "abcdefghijk", URL: "https://youtu.be/abcdefghijk", Duration: 1}, Backend: "local", Model: "small", Text: "Bonjour.\n\nEncore bonjour."}
	for _, format := range []string{"txt", "md", "json"} {
		p := filepath.Join(dir, "out."+format)
		if err := Export(tr, format, p, false); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(p)
		if format == "json" {
			var out core.Transcript
			if err := json.Unmarshal(b, &out); err != nil {
				t.Fatal(err)
			}
			if out.Text != tr.Text || out.Segments == nil || out.Language != nil {
				t.Fatalf("bad roundtrip: %+v", out)
			}
			if !strings.Contains(string(b), `"title": null`) {
				t.Fatal("metadata must be null")
			}
		} else if !strings.Contains(string(b), tr.Text) {
			t.Fatal("text changed")
		}
		if format == "md" {
			for _, label := range []string{"Source: ", "Backend: local", "Model: small"} {
				if !strings.Contains(string(b), label) {
					t.Fatalf("missing English metadata label %q", label)
				}
			}
		}
	}
}
func TestLockCleanAndReacquire(t *testing.T) {
	root := t.TempDir()
	active := filepath.Join(root, "active")
	inactive := filepath.Join(root, "inactive")
	release, err := Acquire(active)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := Acquire(active); err == nil {
		t.Fatal("concurrent writer accepted")
	}
	other, err := Acquire(inactive)
	if err != nil {
		t.Fatal(err)
	}
	other()
	if err := Clean(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(active); err != nil {
		t.Fatal("active deleted")
	}
	if _, err := os.Stat(inactive); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("inactive survived")
	}
	release()
	release()
	next, err := Acquire(active)
	if err != nil {
		t.Fatal(err)
	}
	next()
}
func TestManifestRoundtripAndVersion(t *testing.T) {
	dir := t.TempDir()
	m := Manifest{Backend: "mistral", Completed: map[int]core.Result{2: {Text: "segment terminé"}}}
	if err := Save(dir, m); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Completed[2].Text != m.Completed[2].Text {
		t.Fatal("segment lost")
	}
	if err := WriteJSON(filepath.Join(dir, "manifest.json"), Manifest{Version: 99}); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("unknown version accepted")
	}
}
func TestKeyAndFilename(t *testing.T) {
	if Key("v", "local", "small", "") == Key("v", "mistral", "small", "") {
		t.Fatal("backend collision")
	}
	title := " ../Un titre / étrange \\ et\nlong " + strings.Repeat("é", 200)
	filename := Filename(core.Video{ID: "abcdefghijk", Title: &title}, "txt")
	if strings.ContainsAny(filename, "/\\\n") || len(filename) > 200 {
		t.Fatal(filename)
	}
	if Filename(core.Video{ID: "abcdefghijk"}, "json") != "abcdefghijk.json" {
		t.Fatal("fallback")
	}
}

func TestCrashReleasesLock(t *testing.T) {
	if os.Getenv("YT_CHONK_LOCK_HELPER") == "1" {
		_, err := Acquire(os.Getenv("YT_CHONK_LOCK_DIR"))
		if err != nil {
			os.Exit(3)
		}
		// Exit without releasing: the kernel must release the flock.
		os.Exit(0)
	}
	dir := filepath.Join(t.TempDir(), "crashed")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashReleasesLock$")
	cmd.Env = append(os.Environ(), "YT_CHONK_LOCK_HELPER=1", "YT_CHONK_LOCK_DIR="+dir)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	release, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
func TestFailedWritePreservesPreviousJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := WriteJSON(p, map[string]int{"version": 1}); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(p)
	if err := WriteJSON(p, make(chan int)); err == nil {
		t.Fatal("unsupported value accepted")
	}
	current, _ := os.ReadFile(p)
	if string(current) != string(original) {
		t.Fatal("failed write damaged config")
	}
	stat, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if stat.Mode().Perm() != 0600 {
		t.Fatal("config permissions too broad")
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	if len(entries) != 1 {
		t.Fatal("temporary leaked")
	}
}

func TestCleanConcurrentWithJobAcquisition(t *testing.T) {
	root := t.TempDir()
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 40; i++ {
			if err := Clean(root); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 40; i++ {
		dir := filepath.Join(root, "job")
		release, err := Acquire(dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := Save(dir, Manifest{Completed: map[int]core.Result{0: {Text: "persisté"}}}); err != nil {
			release()
			t.Fatal(err)
		}
		if _, err := Load(dir); err != nil {
			release()
			t.Fatal("active job removed: ", err)
		}
		release()
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestAcquireRootLockCancellation(t *testing.T) {
	root := t.TempDir()
	f, err := lock(filepath.Join(root, ".lock"), true)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock(f)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := AcquireContext(ctx, filepath.Join(root, "job")); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}
