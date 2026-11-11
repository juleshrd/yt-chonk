// Package store owns versioned cache persistence, locks and export publication.
package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"yt-chonk/internal/core"
)

const Version = 1

// Manifest contains only reusable results and non-secret processing parameters.
type Manifest struct {
	ModelHash    string              `json:"model_hash,omitempty"`
	Version      int                 `json:"version"`
	Video        core.Video          `json:"video"`
	Backend      string              `json:"backend"`
	Model        string              `json:"model"`
	Language     string              `json:"language"`
	AudioHash    string              `json:"audio_hash"`
	ChunkSeconds float64             `json:"chunk_seconds"`
	Completed    map[int]core.Result `json:"completed"`
	Transcript   *core.Transcript    `json:"transcript"`
}

// Key excludes export format and destination so a complete transcript can be
// exported offline. Strategy changes must change the versioned prefix.
func Key(videoID, backend, model, language string) string {
	b, _ := json.Marshal([]string{"audio-pcm16-mono16k-chunks-v1", videoID, backend, model, language})
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}

func WriteJSON(path string, v any) error {
	return atomicWrite(path, true, func(w io.Writer) error {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	})
}

func Load(dir string) (Manifest, error) {
	var m Manifest
	f, err := os.Open(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return m, err
	}
	defer f.Close()
	if err := json.NewDecoder(io.LimitReader(f, 64<<20)).Decode(&m); err != nil {
		return m, fmt.Errorf("cannot read cache: %w", err)
	}
	if m.Version != Version {
		return m, fmt.Errorf("unsupported cache version: %d", m.Version)
	}
	if m.Completed == nil {
		m.Completed = make(map[int]core.Result)
	}
	return m, nil
}

func Save(dir string, m Manifest) error {
	m.Version = Version
	return WriteJSON(filepath.Join(dir, "manifest.json"), m)
}

func lock(path string, nonblock bool) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	flags := syscall.LOCK_EX
	if nonblock {
		flags |= syscall.LOCK_NB
	}
	if err = syscall.Flock(int(f.Fd()), flags); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}
func unlock(f *os.File) { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }

// Acquire coordinates directory creation with Clean, then takes a nonblocking
// process-lifetime lock. The lock file is never removed while the job is visible.
func Acquire(dir string) (func(), error) { return AcquireContext(context.Background(), dir) }

func AcquireContext(ctx context.Context, dir string) (func(), error) {
	root := filepath.Dir(dir)
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	rootLock, err := lockContext(ctx, filepath.Join(root, ".lock"))
	if err != nil {
		return nil, err
	}
	defer unlock(rootLock)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	jobLock, err := lock(filepath.Join(dir, ".lock"), true)
	if err != nil {
		return nil, fmt.Errorf("job already active or lock unavailable: %w", err)
	}
	var once sync.Once
	return func() { once.Do(func() { unlock(jobLock) }) }, nil
}

// Clean removes inactive jobs only. Root locking prevents a new process from
// opening an old job lock between inspection and directory removal.
func Clean(root string) error { return CleanContext(context.Background(), root) }

func CleanContext(ctx context.Context, root string) error {
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	rootLock, err := lockContext(ctx, filepath.Join(root, ".lock"))
	if err != nil {
		return err
	}
	defer unlock(rootLock)
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		jobLock, err := lock(filepath.Join(dir, ".lock"), true)
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			continue
		}
		if err != nil {
			return err
		}
		// The root lock makes removal safe without exposing a second lock inode.
		err = os.RemoveAll(dir)
		unlock(jobLock)
		if err != nil {
			return err
		}
	}
	return nil
}

func atomicWrite(path string, force bool, write func(io.Writer) error) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".yt-chonk-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	defer os.Remove(temp)
	defer f.Close()
	if err = write(f); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if force {
		err = os.Rename(temp, path)
	} else {
		err = os.Link(temp, path)
	}
	if err != nil {
		return fmt.Errorf("cannot publish output (use --force to replace): %w", err)
	}
	return nil
}

func Export(t core.Transcript, format, path string, force bool) error {
	if t.Segments == nil {
		t.Segments = []core.Segment{}
	}
	return atomicWrite(path, force, func(w io.Writer) error {
		var err error
		switch format {
		case "txt":
			_, err = io.WriteString(w, t.Text)
		case "md":
			title := t.Video.ID
			if t.Video.Title != nil && *t.Video.Title != "" {
				title = *t.Video.Title
			}
			title = strings.NewReplacer("\n", " ", "\r", " ", "#", "\\#", "<", "&lt;", ">", "&gt;").Replace(title)
			_, err = fmt.Fprintf(w, "# %s\n\nSource: %s\n\nBackend: %s — Model: %s\n\n%s", title, t.Video.URL, t.Backend, t.Model, t.Text)
		case "json":
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			err = enc.Encode(t)
		default:
			return fmt.Errorf("unknown format: %s", format)
		}
		return err
	})
}

func Filename(video core.Video, format string) string {
	title := ""
	if video.Title != nil {
		title = *video.Title
	}
	clean := func(s string) string {
		s = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) || strings.ContainsRune("/\\:*?\"<>|", r) {
				return '-'
			}
			return r
		}, s)
		s = strings.Trim(s, " .\t")
		for len(s) > 160 {
			_, size := utf8.DecodeLastRuneInString(s)
			s = s[:len(s)-size]
		}
		return s
	}
	title = clean(title)
	id := clean(video.ID)
	if id == "" {
		id = "video"
	}
	if title == "" {
		return id + "." + format
	}
	return title + "-" + id + "." + format
}

// Coordinate short root operations while remaining interruptible if another
// process stalls during cleanup.
func lockContext(ctx context.Context, path string) (*os.File, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f, err := lock(path, true)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return nil, err
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
