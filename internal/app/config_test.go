package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"yt-chonk/internal/core"
	"yt-chonk/internal/store"
)

func TestSetupMistralModelChoices(t *testing.T) {
	for _, tt := range []struct {
		name, backend, current, input, want string
	}{
		{"default mini", "", "", "mistral\n\n\nno\n", "voxtral-mini-transcribe-3"},
		{"choose small", "", "", "mistral\n\n2\nno\n", "voxtral-small-transcribe-3"},
		{"keep small", "mistral", "voxtral-small-transcribe-3", "\n\n\nno\n", "voxtral-small-transcribe-3"},
		{"change small to mini", "mistral", "voxtral-small-transcribe-3", "\n\n1\nno\n", "voxtral-mini-transcribe-3"},
		{"switch from local", "local", "small", "mistral\n\n\nno\n", "voxtral-mini-transcribe-3"},
		{"keep custom", "mistral", "custom-model", "\n\n\nno\n", "custom-model"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PATH", t.TempDir()) // Setup needs no tools, keychain or API call.
			dir := filepath.Join(t.TempDir(), "config")
			var out bytes.Buffer
			cfg := Config{Version: 1, Backend: tt.backend, Model: tt.current, Format: "txt"}
			if err := setup(context.Background(), dir, cfg, strings.NewReader(tt.input), &out); err != nil {
				t.Fatal(err)
			}
			saved, err := loadConfig(dir)
			if err != nil || saved.Backend != "mistral" || saved.Model != tt.want {
				t.Fatalf("saved %+v, error %v; want %s", saved, err, tt.want)
			}
			for _, text := range []string{
				"Voxtral Mini Transcribe 3 — USD 0.0035/min | about USD 0.21 for 1 hour of audio",
				"Voxtral Small Transcribe 3 — USD 0.0040/min | about USD 0.24 for 1 hour of audio",
				"checked 2026-11-11", "https://docs.mistral.ai/inference/pricing", "local has no API fee",
			} {
				if !strings.Contains(out.String(), text) {
					t.Fatalf("missing %q in setup: %s", text, &out)
				}
			}
			if tt.want == "custom-model" && !strings.Contains(out.String(), "custom-model — pricing unavailable") {
				t.Fatal("custom model was given an unsupported price")
			}
		})
	}
}

func TestSetupInvalidModelPreservesConfiguration(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{Version: 1, Backend: "mistral", Model: "voxtral-small-transcribe-3", Format: "txt"}
	path := filepath.Join(dir, "config.json")
	if err := store.WriteJSON(path, cfg); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	err = setup(context.Background(), dir, cfg, strings.NewReader("\njson\n9\n"), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "Choose a listed model number") {
		t.Fatalf("unexpected error: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("invalid choice replaced existing configuration: %v", err)
	}
}

func TestTranscribeUsesSetupModelAndCLIOverride(t *testing.T) {
	isolate(t)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("MISTRAL_API_KEY", "")
	confDir, cacheDir, err := dirs()
	if err != nil {
		t.Fatal(err)
	}
	if err := setup(context.Background(), confDir, Config{Format: "txt"}, strings.NewReader("mistral\n\n2\nno\n"), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	// Distinct offline results prove which model/cache the CLI actually selects.
	for _, model := range []string{"voxtral-mini-transcribe-3", "voxtral-small-transcribe-3"} {
		dir := filepath.Join(cacheDir, store.Key("abcdefghijk", "mistral", model, ""))
		release, err := store.Acquire(dir)
		if err != nil {
			t.Fatal(err)
		}
		video := core.Video{ID: "abcdefghijk", Duration: 1}
		err = store.Save(dir, store.Manifest{Backend: "mistral", Model: model, Video: video, ChunkSeconds: 300,
			Transcript: &core.Transcript{SchemaVersion: 1, Backend: "mistral", Model: model, Video: video, Text: model}})
		release()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, override := range []bool{false, true} {
		want := "voxtral-small-transcribe-3"
		output := filepath.Join(t.TempDir(), "transcript.txt")
		args := []string{"transcribe", "https://youtu.be/abcdefghijk", "--output", output}
		if override {
			want = "voxtral-mini-transcribe-3"
			args = append(args, "--model", want)
		}
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), args, strings.NewReader(""), &stdout, &stderr); code != 0 {
			t.Fatalf("exit %d: %s", code, &stderr)
		}
		text, err := os.ReadFile(output)
		if err != nil || strings.TrimSpace(string(text)) != want {
			t.Fatalf("got %q, error %v; want %s", text, err, want)
		}
	}
	saved, err := loadConfig(confDir)
	if err != nil || saved.Model != "voxtral-small-transcribe-3" {
		t.Fatalf("CLI override changed saved model: %+v, %v", saved, err)
	}
}
