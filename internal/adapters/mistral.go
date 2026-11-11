package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"yt-chonk/internal/core"
)

type Mistral struct {
	Key     string
	BaseURL string
	Client  *http.Client
}

func (m Mistral) Transcribe(ctx context.Context, path, model, language string) (core.Result, error) {
	if strings.TrimSpace(m.Key) == "" {
		return core.Result{}, fmt.Errorf("Mistral API key missing: set MISTRAL_API_KEY or run setup")
	}
	if model == "" {
		model = "voxtral-mini-transcribe-3"
	}
	for attempt := 0; attempt < 4; attempt++ {
		r, status, retry, err := m.once(ctx, path, model, language)
		if err == nil {
			return r, nil
		}
		if ctx.Err() != nil {
			return core.Result{}, ctx.Err()
		}
		if attempt == 3 || !(status == 0 || status == 408 || status == 429 || status >= 500) {
			return core.Result{}, err
		}
		delay := time.Second * time.Duration(1<<attempt)
		if retry > delay {
			delay = retry
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return core.Result{}, ctx.Err()
		case <-timer.C:
		}
	}
	panic("unreachable")
}

func (m Mistral) once(ctx context.Context, path, model, language string) (core.Result, int, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	f, err := os.Open(path)
	if err != nil {
		return core.Result{}, -1, 0, fmt.Errorf("audio chunk not found")
	}
	defer f.Close()
	pr, pw := io.Pipe()
	defer pr.Close()
	writer := multipart.NewWriter(pw)
	reqURL := strings.TrimRight(m.BaseURL, "/")
	if reqURL == "" {
		reqURL = "https://api.mistral.ai"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL+"/v1/audio/transcriptions", pr)
	if err != nil {
		pw.Close()
		return core.Result{}, -1, 0, fmt.Errorf("invalid Mistral API address")
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+m.Key)
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := writer.WriteField("model", model)
		if err == nil && language != "" {
			err = writer.WriteField("language", language)
		}
		if err == nil {
			err = writer.WriteField("timestamp_granularities", "segment")
		}
		if err == nil && language == "" {
			err = writer.WriteField("return_language", "true")
		}
		if err == nil {
			var part io.Writer
			part, err = writer.CreateFormFile("file", filepath.Base(path))
			if err == nil {
				_, err = io.Copy(part, f)
			}
		}
		if err == nil {
			err = writer.Close()
		}
		pw.CloseWithError(err)
	}()
	client := m.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	resp, err := client.Do(req)
	// Unblock the multipart producer even if a server rejects an upload early.
	pr.Close()
	<-done
	if err != nil {
		return core.Result{}, 0, 0, fmt.Errorf("Mistral request interrupted: check the network; remote processing may have been billed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		retry := retryAfter(resp.Header.Get("Retry-After"), time.Now())
		switch resp.StatusCode {
		case 401, 403:
			return core.Result{}, resp.StatusCode, retry, fmt.Errorf("Mistral authentication rejected: check MISTRAL_API_KEY or the system keyring")
		default:
			return core.Result{}, resp.StatusCode, retry, fmt.Errorf("Mistral API: HTTP status %d", resp.StatusCode)
		}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxOutput+1))
	if err != nil || len(data) > maxOutput {
		return core.Result{}, -1, 0, fmt.Errorf("Mistral response is incomplete or too large")
	}
	var r core.Result
	var shape struct {
		Text *string `json:"text"`
	}
	if json.Unmarshal(data, &shape) != nil || shape.Text == nil || json.Unmarshal(data, &r) != nil {
		return core.Result{}, -1, 0, fmt.Errorf("invalid Mistral response")
	}
	if r.Model == "" {
		r.Model = model
	}
	if r.Segments == nil {
		r.Segments = []core.Segment{}
	}
	if language != "" {
		r.Language = &language
	}
	return r, resp.StatusCode, 0, nil
}
func retryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return date.Sub(now)
	}
	return 0
}
