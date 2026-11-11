package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"yt-chonk/internal/adapters"
	"yt-chonk/internal/core"
	"yt-chonk/internal/store"
)

var clean = store.CleanContext
var filename = store.Filename

func hashFile(p string) (string, error) {
	f, e := os.Open(p)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func transcribe(ctx context.Context, root, id, url string, o options, diag io.Writer) (string, error) {
	progress := func(s string, a ...any) {
		if !o.quiet {
			fmt.Fprintf(diag, s+"\n", a...)
		}
	}
	modelKey := o.model
	if o.backend == "local" && o.localModel != "" {
		abs, _ := filepath.Abs(o.localModel)
		modelKey += "@" + abs
	}
	dir := filepath.Join(root, store.Key(id, o.backend, modelKey, o.language))
	release, e := store.AcquireContext(ctx, dir)
	if e != nil {
		return "", e
	}
	defer release()
	m, e := store.Load(dir)
	fresh := os.IsNotExist(e)
	if e != nil && !fresh {
		return "", e
	}
	if fresh {
		m = store.Manifest{Version: 1, Backend: o.backend, Model: modelKey, Language: o.language, ChunkSeconds: 300, Completed: map[int]core.Result{}}
	} else if m.Backend != o.backend || m.Model != modelKey || m.Language != o.language || m.ChunkSeconds != 300 || m.Video.ID != id {
		return "", errors.New("Incompatible cache. Run yt-chonk cache clean to start again")
	}

	if o.backend == "local" && m.ModelHash != "" {
		if h, err := hashFile(o.localModel); err == nil && h != m.ModelHash {
			return "", errors.New("Local model has changed. Run yt-chonk cache clean before trying again")
		}
	}
	if m.Transcript == nil && m.AudioHash != "" && validDuration(m.Video.Duration) == nil {
		count := int(math.Ceil(m.Video.Duration / m.ChunkSeconds))
		complete := true
		for i := 0; i < count; i++ {
			r, ok := m.Completed[i]
			if !ok {
				complete = false
				break
			}
			if err := validateResult(r, math.Min(m.ChunkSeconds, m.Video.Duration-float64(i)*m.ChunkSeconds)); err != nil {
				return "", err
			}
		}
		if complete {
			path, err := destination(o, m.Video)
			if err != nil {
				return "", err
			}
			return publish(dir, path, o, m)
		}
	}
	if m.Transcript != nil {
		path, e := destination(o, m.Video)
		if e != nil {
			return "", e
		}
		if e = store.Export(*m.Transcript, o.format, path, o.force); e != nil {
			return "", e
		}
		removeAudio(dir)
		return path, nil
	}
	if o.output != "" {
		if _, e = destination(o, core.Video{ID: id}); e != nil {
			return "", e
		}
	}
	if m.Video.ID == "" {
		if e = required("yt-dlp", "deno"); e != nil {
			return "", e
		}
		progress("Reading YouTube metadata...")
		metaCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		m.Video, e = adapters.Metadata(metaCtx, url)
		cancel()
		if e != nil {
			return "", e
		}
		if m.Video.ID != id {
			return "", errors.New("YouTube returned an unexpected video ID")
		}
		m.Video.URL = url
		if e = validDuration(m.Video.Duration); e != nil {
			return "", e
		}
		if e = store.Save(dir, m); e != nil {
			return "", e
		}
	}
	path, e := destination(o, m.Video)
	if e != nil {
		return "", e
	}
	audio := filepath.Join(dir, "prepared.wav")
	hash, e := hashFile(audio)
	ready := e == nil && m.AudioHash != "" && hash == m.AudioHash
	if !ready && len(m.Completed) > 0 {
		return "", errors.New("Cached audio is missing or has changed. Run yt-chonk cache clean to start again")
	}
	var engine core.Engine
	if o.backend == "mistral" {
		k := key(ctx)
		if k == "" {
			return "", configError{"Missing API key. Set MISTRAL_API_KEY or run yt-chonk setup"}
		}
		engine = adapters.Mistral{Key: k}
	} else {
		if e = required("whisper-cli"); e != nil {
			return "", e
		}
		if o.localModel == "" {
			return "", configError{"Missing local model. Run yt-chonk setup or pass --model /path/to/ggml-small.bin"}
		}
		if s, e := os.Stat(o.localModel); e != nil || s.IsDir() {
			return "", configError{"Local model not found. Check --model or run yt-chonk setup"}
		}
		m.ModelHash, e = hashFile(o.localModel)
		if e != nil {
			return "", e
		}
		engine = adapters.Local{ModelPath: o.localModel}
	}
	if e = required("ffmpeg", "ffprobe"); e != nil {
		return "", e
	}
	if !ready {
		source := filepath.Join(dir, "source.audio")
		if _, e = os.Stat(source); os.IsNotExist(e) {
			if e = required("yt-dlp", "deno"); e != nil {
				return "", e
			}
			progress("Downloading audio...")
			dlCtx, cancel := context.WithTimeout(ctx, 45*time.Minute)
			e = adapters.Download(dlCtx, url, source)
			cancel()
			if e != nil {
				return "", e
			}
		}
		progress("Preparing audio...")
		prepCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
		e = adapters.Prepare(prepCtx, source, audio)
		cancel()
		if e != nil {
			return "", e
		}
		m.AudioHash, e = hashFile(audio)
		if e != nil {
			return "", e
		}
	}
	durCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	duration, e := adapters.Duration(durCtx, audio)
	cancel()
	if e != nil {
		return "", e
	}
	if e = validDuration(duration); e != nil {
		return "", e
	}
	m.Video.Duration = duration
	if e = store.Save(dir, m); e != nil {
		return "", e
	}
	count := int(math.Ceil(duration / m.ChunkSeconds))
	lang := o.language
	for i := 0; i < count; i++ {
		if r, ok := m.Completed[i]; ok {
			if lang == "" && r.Language != nil {
				lang = *r.Language
			}
			continue
		}
		if e = ctx.Err(); e != nil {
			return "", e
		}
		start := float64(i) * m.ChunkSeconds
		length := math.Min(m.ChunkSeconds, duration-start)
		chunk := filepath.Join(dir, "chunk.wav")
		progress("Transcribing chunk %d/%d...", i+1, count)
		chunkCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		e = adapters.Slice(chunkCtx, audio, chunk, start, length)
		cancel()
		if e != nil {
			return "", e
		}
		callCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
		r, err := engine.Transcribe(callCtx, chunk, o.model, lang)
		cancel()
		if err != nil {
			return "", err
		}
		if e = validateResult(r, length); e != nil {
			return "", e
		}
		if lang == "" && r.Language != nil {
			lang = *r.Language
		}
		m.Completed[i] = r
		if e = store.Save(dir, m); e != nil {
			return "", e
		}
		_ = os.Remove(chunk)
	}
	return publish(dir, path, o, m)
}
func publish(dir, path string, o options, m store.Manifest) (string, error) {
	count := int(math.Ceil(m.Video.Duration / m.ChunkSeconds))
	t := core.Transcript{SchemaVersion: 1, Video: m.Video, Backend: o.backend, Model: o.model, Segments: []core.Segment{}}
	mixed := false
	texts := make([]string, 0, count)
	for i := 0; i < count; i++ {
		r, ok := m.Completed[i]
		if !ok {
			return "", errors.New("Incomplete cache")
		}
		if r.Model != "" {
			t.Model = r.Model
		}
		if r.Language != nil && !mixed {
			if t.Language == nil {
				v := *r.Language
				t.Language = &v
			} else if *t.Language != *r.Language {
				t.Language = nil
				mixed = true
			}
		}
		texts = append(texts, strings.TrimSpace(r.Text))
		for _, s := range r.Segments {
			s.Start += float64(i) * m.ChunkSeconds
			s.End += float64(i) * m.ChunkSeconds
			t.Segments = append(t.Segments, s)
		}
	}
	if o.language != "" {
		v := o.language
		t.Language = &v
	}
	t.Text = strings.Join(texts, "\n\n")
	m.Transcript = &t
	if e := store.Save(dir, m); e != nil {
		return "", e
	}
	if e := store.Export(t, o.format, path, o.force); e != nil {
		return "", e
	}
	removeAudio(dir)
	return path, nil
}
func validDuration(d float64) error {
	if math.IsNaN(d) || math.IsInf(d, 0) || d <= 0 {
		return errors.New("Unknown or invalid audio duration")
	}
	if d > 14400 {
		return errors.New("Video exceeds the four-hour limit")
	}
	return nil
}
func validateResult(r core.Result, duration float64) error {
	last := 0.0
	for _, s := range r.Segments {
		if math.IsNaN(s.Start) || math.IsNaN(s.End) || math.IsInf(s.Start, 0) || math.IsInf(s.End, 0) || s.Start < last || s.End < s.Start || s.End > duration {
			return errors.New("Backend returned invalid timestamps")
		}
		last = s.Start
	}
	return nil
}
func removeAudio(dir string) {
	for _, n := range []string{"prepared.wav", "source.audio", "chunk.wav"} {
		_ = os.Remove(filepath.Join(dir, n))
	}
}
