package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yt-chonk/internal/core"
	"yt-chonk/internal/store"
)

func TestIdentityCanonicalizesOnlyYouTubeVideos(t *testing.T) {
	const id = "abcdefghijk"
	for _, raw := range []string{"https://youtu.be/" + id + "?list=ignored", "https://www.youtube.com/watch?v=" + id + "&list=ignored", "https://m.youtube.com/shorts/" + id, "https://youtube.com/embed/" + id} {
		got, url, err := identity(raw)
		if err != nil || got != id || url != "https://www.youtube.com/watch?v="+id {
			t.Fatalf("%s: %s %s %v", raw, got, url, err)
		}
	}
	for _, raw := range []string{"https://evil.example/watch?v=" + id, "https://youtube.com.evil.example/watch?v=" + id, "https://user@youtube.com/watch?v=" + id, "file:///watch?v=" + id, "https://youtube.com/playlist?list=PL123", "https://youtu.be/short", "https://youtube.com:123/watch?v=" + id} {
		if _, _, err := identity(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
func isolate(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
}
func writeFile(t *testing.T, path, text string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), mode); err != nil {
		t.Fatal(err)
	}
}
func TestCLIUsageAndNoImplicitCloud(t *testing.T) {
	isolate(t)
	t.Setenv("MISTRAL_API_KEY", "must-not-select-cloud")
	for _, args := range [][]string{{"transcribe", "https://youtu.be/abcdefghijk"}, {"transcribe", "https://youtu.be/abcdefghijk", "--backend", "local", "--format", "srt"}, {"transcribe", "https://youtu.be/abcdefghijk", "--backend", "local", "--language", "FR"}} {
		var stdout, stderr bytes.Buffer
		if got := Run(context.Background(), args, strings.NewReader(""), &stdout, &stderr); got != 2 {
			t.Fatalf("%v: got %d: %s", args, got, stderr.String())
		}
		if stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatal("error output contract violated")
		}
	}
	var stdout, stderr bytes.Buffer
	if Run(context.Background(), []string{"--help"}, strings.NewReader(""), &stdout, &stderr) != 0 || !strings.Contains(stdout.String(), "transcribe") || stderr.Len() != 0 {
		t.Fatal("help contract")
	}
}
func TestLocalResumeAndOfflineCLIExport(t *testing.T) {
	isolate(t)
	root := t.TempDir()
	bin := filepath.Join(root, "fake bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(root, "calls")
	marker := filepath.Join(root, "failed")
	t.Setenv("FAKE_LOG", log)
	t.Setenv("FAKE_MARKER", marker)
	t.Setenv("PATH", bin)
	scripts := map[string]string{
		"deno": `exit 0`,
		"yt-dlp": `
echo "yt-dlp" >> "$FAKE_LOG"
meta=0
out=
while [ "$#" -gt 0 ]; do
 case "$1" in --dump-single-json) meta=1 ;; -o) shift; out=$1 ;; esac
 shift
done
if [ "$meta" = 1 ]; then
 echo '{"id":"abcdefghijk","title":"Titre / inhabituel","channel":"Une chaîne","duration":601}'
else
 printf audio > "$out"
fi`,
		"ffmpeg": `
echo "ffmpeg" >> "$FAKE_LOG"
start=prepared
while [ "$#" -gt 0 ]; do
 case "$1" in -ss) shift; start=$1 ;; esac
 out=$1
 shift
done
printf '%s\n' "$start" > "$out"`,
		"ffprobe": `echo '{"format":{"duration":"601"}}'`,
		"whisper-cli": `
while [ "$#" -gt 0 ]; do
 case "$1" in -of) shift; prefix=$1 ;; -f) shift; chunk=$1 ;; esac
 shift
done
read -r start < "$chunk"
echo "whisper:$start" >> "$FAKE_LOG"
if [ "$start" = 300.000000 ] && [ ! -f "$FAKE_MARKER" ]; then
 printf failed > "$FAKE_MARKER"
 exit 1
fi
printf '{"result":{"language":"fr"},"transcription":[{"text":"morceau-%s","offsets":{"from":0,"to":500}}]}' "$start" > "$prefix.json"`,
	}
	for name, script := range scripts {
		writeFile(t, filepath.Join(bin, name), "#!/bin/sh\n"+script+"\n", 0700)
	}
	model := filepath.Join(root, "modèle small.bin")
	writeFile(t, model, "model", 0600)
	confDir, cacheDir, err := dirs()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(confDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteJSON(filepath.Join(confDir, "config.json"), Config{Version: 1, Backend: "local", Model: "small", LocalModel: model, Format: "txt"}); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "résultat avec espaces.json")
	args := []string{"transcribe", "https://youtu.be/abcdefghijk", "--format", "json", "--output", output, "--quiet"}
	var stdout, stderr bytes.Buffer
	if got := Run(context.Background(), args, strings.NewReader(""), &stdout, &stderr); got != 1 {
		t.Fatalf("first must fail: %d %s", got, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatal("failure wrote stdout")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("partial final export exists")
	}
	absModel, _ := filepath.Abs(model)
	job := filepath.Join(cacheDir, store.Key("abcdefghijk", "local", "small@"+absModel, ""))
	m, err := store.Load(job)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Completed) != 1 {
		t.Fatalf("expected one completed chunk, got %d", len(m.Completed))
	}
	stdout.Reset()
	stderr.Reset()
	if got := Run(context.Background(), args, strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("resume failed: %d %s", got, stderr.String())
	}
	if stdout.String() != output+"\n" || stderr.Len() != 0 {
		t.Fatalf("quiet output contract: %q %q", stdout.String(), stderr.String())
	}
	b, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var tr core.Transcript
	if err := json.Unmarshal(b, &tr); err != nil {
		t.Fatal(err)
	}
	if len(tr.Segments) != 3 || tr.Segments[1].Start != 300 || tr.Segments[2].End != 600.5 {
		t.Fatalf("wrong global offsets: %+v", tr.Segments)
	}
	calls, _ := os.ReadFile(log)
	if strings.Count(string(calls), "whisper:") != 4 || strings.Count(string(calls), "whisper:0.000000") != 1 {
		t.Fatal("completed chunk was re-transcribed: ", string(calls))
	}
	if _, err := os.Stat(filepath.Join(job, "prepared.wav")); !os.IsNotExist(err) {
		t.Fatal("audio retained after successful export")
	}
	t.Setenv("PATH", t.TempDir())
	if err := os.Remove(model); err != nil {
		t.Fatal(err)
	}
	offline := filepath.Join(root, "offline.txt")
	stdout.Reset()
	stderr.Reset()
	if got := Run(context.Background(), []string{"transcribe", "https://www.youtube.com/watch?v=abcdefghijk&list=ignored", "--format", "txt", "--output", offline, "--quiet"}, strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("offline export requires dependencies: %d %s", got, stderr.String())
	}
	text, err := os.ReadFile(offline)
	if err != nil || string(text) != tr.Text {
		t.Fatal("offline text differs")
	}
	stdout.Reset()
	stderr.Reset()
	if got := Run(context.Background(), args, strings.NewReader(""), &stdout, &stderr); got != 1 || !strings.Contains(stderr.String(), "Output already exists") {
		t.Fatalf("collision was not detected: %d %s", got, stderr.String())
	}
}
func TestDurationAndTimestampValidation(t *testing.T) {
	if err := validDuration(14400); err != nil {
		t.Fatal(err)
	}
	for _, v := range []float64{14400.001, 0, -1, math.NaN(), math.Inf(1)} {
		if validDuration(v) == nil {
			t.Fatalf("invalid duration %v accepted", v)
		}
	}
	for _, segments := range [][]core.Segment{{{Start: -1, End: 1}}, {{Start: 1, End: 0}}, {{Start: 0, End: math.NaN()}}, {{Start: 0, End: 2}}, {{Start: .5, End: .7}, {Start: .3, End: .8}}} {
		if validateResult(core.Result{Segments: segments}, 1) == nil {
			t.Fatal("invalid timestamps accepted")
		}
	}
}

func TestSetupCreatesConfigurationDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "new config", "yt-chonk")
	var diagnostics bytes.Buffer
	if err := setup(context.Background(), dir, Config{Version: 1, Format: "txt"}, strings.NewReader("local\njson\n/path/ggml-small.bin\n"), &diagnostics); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Backend != "local" || cfg.Format != "json" || cfg.LocalModel != "/path/ggml-small.bin" {
		t.Fatalf("configuration not saved: %+v", cfg)
	}
}

func TestOfflineAssemblesAllCompletedChunksAfterCrash(t *testing.T) {
	root := t.TempDir()
	model := filepath.Join(root, "removed model.bin")
	o := options{backend: "local", model: "small", localModel: model, format: "json", output: filepath.Join(root, "offline.json"), quiet: true}
	dir := filepath.Join(root, store.Key("abcdefghijk", "local", "small@"+model, ""))
	release, err := store.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	fr, en := "fr", "en"
	m := store.Manifest{Video: core.Video{ID: "abcdefghijk", URL: "https://www.youtube.com/watch?v=abcdefghijk", Duration: 601}, Backend: "local", Model: "small@" + model, ModelHash: "hash-from-now-absent-model", AudioHash: "hash-from-now-absent-audio", ChunkSeconds: 300, Completed: map[int]core.Result{
		0: {Text: "Premier.", Language: &fr, Model: "small", Segments: []core.Segment{{Start: 0, End: 1, Text: "Premier."}}},
		1: {Text: "Second.", Language: &en, Model: "small", Segments: []core.Segment{{Start: 0, End: 1, Text: "Second."}}},
		2: {Text: "Troisième.", Language: &fr, Model: "small", Segments: []core.Segment{{Start: 0, End: 1, Text: "Troisième."}}},
	}}
	if err := store.Save(dir, m); err != nil {
		release()
		t.Fatal(err)
	}
	release()
	// Simulate a crash after the final chunk save, before aggregate persistence.
	// Neither audio, model, executable nor API key is available for recovery.
	t.Setenv("PATH", t.TempDir())
	t.Setenv("MISTRAL_API_KEY", "")
	var diagnostics bytes.Buffer
	path, err := transcribe(context.Background(), root, "abcdefghijk", m.Video.URL, o, &diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var tr core.Transcript
	if err := json.Unmarshal(b, &tr); err != nil {
		t.Fatal(err)
	}
	if tr.Text != "Premier.\n\nSecond.\n\nTroisième." || len(tr.Segments) != 3 || tr.Segments[2].End != 601 {
		t.Fatalf("invalid recovery: %+v", tr)
	}
	if tr.Language != nil {
		t.Fatalf("mixed languages became certain: %s", *tr.Language)
	}
	saved, err := store.Load(dir)
	if err != nil || saved.Transcript == nil {
		t.Fatalf("aggregate not persisted: %v", err)
	}
	if diagnostics.Len() != 0 {
		t.Fatal("quiet recovery emitted diagnostics")
	}
}

func TestReplacedLocalModelInvalidatesCacheAtSamePath(t *testing.T) {
	for _, aggregated := range []bool{false, true} {
		name := "completed chunks"
		if aggregated {
			name = "complete transcript"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			model := filepath.Join(root, "same model.bin")
			writeFile(t, model, "original model bytes", 0600)
			digest, err := hashFile(model)
			if err != nil {
				t.Fatal(err)
			}
			o := options{backend: "local", model: "small", localModel: model, format: "txt", output: filepath.Join(root, "must-not-exist.txt"), quiet: true}
			dir := filepath.Join(root, store.Key("abcdefghijk", "local", "small@"+model, ""))
			release, err := store.Acquire(dir)
			if err != nil {
				t.Fatal(err)
			}
			m := store.Manifest{Video: core.Video{ID: "abcdefghijk", Duration: 1}, Backend: "local", Model: "small@" + model, ModelHash: digest, AudioHash: "prepared-audio-hash", ChunkSeconds: 300, Completed: map[int]core.Result{0: {Text: "Old cached result."}}}
			if aggregated {
				m.Transcript = &core.Transcript{SchemaVersion: 1, Video: m.Video, Text: "Old cached result."}
			}
			if err := store.Save(dir, m); err != nil {
				release()
				t.Fatal(err)
			}
			release()
			writeFile(t, model, "new model bytes at the very same path", 0600)
			t.Setenv("PATH", t.TempDir())
			_, err = transcribe(context.Background(), root, "abcdefghijk", "https://youtu.be/abcdefghijk", o, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "has changed") {
				t.Fatalf("changed model accepted: %v", err)
			}
			if _, err := os.Stat(o.output); !os.IsNotExist(err) {
				t.Fatal("incompatible result exported")
			}
		})
	}
}

type promptNotification chan struct{}

func (n promptNotification) Write(p []byte) (int, error) {
	select {
	case n <- struct{}{}:
	default:
	}
	return len(p), nil
}

func TestSetupCancellationDoesNotWriteConfiguration(t *testing.T) {
	isolate(t)
	input, writer := io.Pipe()
	defer input.Close()
	defer writer.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	notified := make(promptNotification, 1)
	done := make(chan int, 1)
	go func() { done <- Run(ctx, []string{"setup"}, input, io.Discard, notified) }()
	select {
	case <-notified:
	case <-time.After(2 * time.Second):
		t.Fatal("setup never prompted")
	}
	cancel()
	select {
	case code := <-done:
		if code != 130 {
			t.Fatalf("interrupt returned %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("setup remained blocked reading terminal after cancellation")
	}
	confDir, _, err := dirs()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(confDir, "config.json")); !os.IsNotExist(err) {
		t.Fatal("interrupted setup persisted configuration")
	}
}
