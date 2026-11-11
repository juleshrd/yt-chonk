package adapters

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func audioFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "audio test.wav")
	if err := os.WriteFile(p, []byte("audio-bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}
func fake(t *testing.T, name, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestMistralMultipart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("missing auth")
		}
		if err := r.ParseMultipartForm(10000); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		defer r.MultipartForm.RemoveAll()
		if r.FormValue("model") != "custom" || r.FormValue("language") != "fr" || r.FormValue("timestamp_granularities") != "segment" {
			t.Error("bad request fields")
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			t.Error(err)
		} else {
			f.Close()
		}
		fmt.Fprint(w, `{"text":"Bonjour.","model":"custom","segments":[{"start":0,"end":1,"text":"Bonjour."}]}`)
	}))
	defer server.Close()
	r, err := (Mistral{Key: "secret", BaseURL: server.URL}).Transcribe(context.Background(), audioFile(t), "custom", "fr")
	if err != nil || r.Text != "Bonjour." || r.Language == nil || *r.Language != "fr" || len(r.Segments) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}
func TestMistralPermanentNoRetryNoSecret(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(401)
		fmt.Fprint(w, "supersecret")
	}))
	defer server.Close()
	_, err := (Mistral{Key: "supersecret", BaseURL: server.URL}).Transcribe(context.Background(), audioFile(t), "custom", "")
	if calls != 1 || err == nil || strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
func TestMistralRetryAfterCancellation(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := (Mistral{Key: "key", BaseURL: server.URL}).Transcribe(ctx, audioFile(t), "custom", "")
	if err != context.DeadlineExceeded || calls != 1 || time.Since(start) > time.Second {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
func TestMistralRetryLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("backoff")
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(503) }))
	defer server.Close()
	_, err := (Mistral{Key: "key", BaseURL: server.URL}).Transcribe(context.Background(), audioFile(t), "custom", "")
	if err == nil || calls != 4 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
func TestLocalNormalization(t *testing.T) {
	fake(t, "whisper-cli", `while [ "$#" -gt 0 ]; do if [ "$1" = '-of' ]; then shift; dest="$1"; fi; shift; done
cat > "$dest.json" <<'JSON'
{"result":{"language":"fr"},"transcription":[{"offsets":{"from":250,"to":1500},"text":" Bonjour."},{"offsets":{"from":1500,"to":2000},"text":" Salut."}]}
JSON
`)
	r, err := (Local{ModelPath: "model with spaces"}).Transcribe(context.Background(), audioFile(t), "small", "")
	if err != nil || r.Text != "Bonjour. Salut." || r.Segments[0].Start != .25 || r.Segments[1].End != 2 {
		t.Fatalf("%+v %v", r, err)
	}
}
func TestMetadataDurationAndLive(t *testing.T) {
	for _, tc := range []struct {
		duration int
		live     bool
		ok       bool
	}{{14400, false, true}, {14401, false, false}, {100, true, false}} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			fake(t, "yt-dlp", fmt.Sprintf("printf '%%s' '%s'", fmt.Sprintf(`{"id":"abc12345678","title":"titre","duration":%d,"is_live":%t}`, tc.duration, tc.live)))
			v, err := Metadata(context.Background(), "https://youtube.com/watch?v=abc12345678")
			if (err == nil) != tc.ok {
				t.Fatalf("%+v %v", v, err)
			}
		})
	}
}
func TestRunCancellationKillsDescendants(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "survived")
	fake(t, "child", `(sleep 0.3; touch "$1") &
wait
`)
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Millisecond)
	defer cancel()
	_, err := Run(ctx, "child", marker)
	if err != context.DeadlineExceeded {
		t.Fatal(err)
	}
	time.Sleep(400 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("descendant survived cancellation")
	}
}
func TestRunErrorsHideChildOutput(t *testing.T) {
	fake(t, "bad", `echo secret >&2; echo secret; exit 1`)
	_, err := Run(context.Background(), "bad")
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal(err)
	}
}
func TestRetryAfter(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	if retryAfter("3", now) != 3*time.Second || retryAfter(now.Add(5*time.Second).Format(http.TimeFormat), now) != 5*time.Second {
		t.Fatal("invalid retry delay")
	}
}

func TestMistralRejectsMissingText(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `{"text":null}`, `not json`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
			defer server.Close()
			_, err := (Mistral{Key: "key", BaseURL: server.URL}).Transcribe(context.Background(), audioFile(t), "custom", "")
			if err == nil {
				t.Fatal("malformed API result accepted")
			}
		})
	}
}
func TestLocalRejectsMissingTranscription(t *testing.T) {
	fake(t, "whisper-cli", `while [ "$#" -gt 0 ]; do if [ "$1" = '-of' ]; then shift; dest="$1"; fi; shift; done
printf '{}' > "$dest.json"
`)
	_, err := (Local{ModelPath: "model"}).Transcribe(context.Background(), audioFile(t), "small", "")
	if err == nil {
		t.Fatal("missing transcription accepted")
	}
}
func TestLocalSilence(t *testing.T) {
	fake(t, "whisper-cli", `while [ "$#" -gt 0 ]; do if [ "$1" = '-of' ]; then shift; dest="$1"; fi; shift; done
printf '{"transcription":[]}' > "$dest.json"
`)
	r, err := (Local{ModelPath: "model"}).Transcribe(context.Background(), audioFile(t), "small", "")
	if err != nil || r.Text != "" || len(r.Segments) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}
