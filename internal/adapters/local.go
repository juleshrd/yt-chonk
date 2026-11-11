package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"yt-chonk/internal/core"
)

type Local struct{ ModelPath string }

func (l Local) Transcribe(ctx context.Context, path, model, language string) (core.Result, error) {
	dir, err := os.MkdirTemp(filepath.Dir(path), "whisper-")
	if err != nil {
		return core.Result{}, err
	}
	defer os.RemoveAll(dir)
	if language == "" {
		language = "auto"
	}
	prefix := filepath.Join(dir, "result")
	_, err = Run(ctx, "whisper-cli", "-m", l.ModelPath, "-f", path, "-l", language, "-oj", "-of", prefix, "-np")
	if err != nil {
		return core.Result{}, err
	}
	f, err := os.Open(prefix + ".json")
	if err != nil {
		return core.Result{}, fmt.Errorf("whisper-cli did not produce JSON output")
	}
	defer f.Close()
	var raw struct {
		Result struct {
			Language string `json:"language"`
		} `json:"result"`
		Transcription []struct {
			Text    string `json:"text"`
			Offsets struct {
				From float64 `json:"from"`
				To   float64 `json:"to"`
			} `json:"offsets"`
		} `json:"transcription"`
	}
	if err := json.NewDecoder(io.LimitReader(f, maxOutput)).Decode(&raw); err != nil || raw.Transcription == nil {
		return core.Result{}, fmt.Errorf("invalid whisper-cli JSON output")
	}
	r := core.Result{Model: model, Segments: []core.Segment{}}
	if raw.Result.Language != "" {
		r.Language = &raw.Result.Language
	}
	var text strings.Builder
	for _, s := range raw.Transcription {
		text.WriteString(s.Text)
		r.Segments = append(r.Segments, core.Segment{Start: s.Offsets.From / 1000, End: s.Offsets.To / 1000, Text: s.Text})
	}
	r.Text = strings.TrimSpace(text.String())
	return r, nil
}
