package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"
	"yt-chonk/internal/core"
)

func Metadata(ctx context.Context, url string) (core.Video, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	data, err := Run(ctx, "yt-dlp", "--ignore-config", "--no-playlist", "--skip-download", "--dump-single-json", "--", url)
	if err != nil {
		return core.Video{}, err
	}
	var v struct {
		ID         string  `json:"id"`
		Title      *string `json:"title"`
		Channel    *string `json:"channel"`
		Duration   float64 `json:"duration"`
		IsLive     bool    `json:"is_live"`
		LiveStatus string  `json:"live_status"`
	}
	if json.Unmarshal(data, &v) != nil {
		return core.Video{}, fmt.Errorf("invalid YouTube metadata")
	}
	if v.IsLive || v.LiveStatus == "is_live" || v.LiveStatus == "is_upcoming" {
		return core.Video{}, fmt.Errorf("live streams and upcoming videos are not supported")
	}
	if v.ID == "" || v.Duration <= 0 || math.IsNaN(v.Duration) || v.Duration > 14400 {
		return core.Video{}, fmt.Errorf("invalid video or duration exceeds four hours")
	}
	return core.Video{ID: v.ID, URL: "https://www.youtube.com/watch?v=" + v.ID, Title: v.Title, Channel: v.Channel, Duration: v.Duration}, nil
}
func Download(ctx context.Context, url, dest string) error {
	// A fixed staging name avoids treating a partially downloaded file as complete.
	tmp := dest + ".download"
	_, err := Run(ctx, "yt-dlp", "--ignore-config", "--no-playlist", "--no-progress", "--retries", "3", "--fragment-retries", "3", "--extractor-retries", "3", "--no-part", "--force-overwrites", "-f", "bestaudio/best", "-o", tmp, "--", url)
	if err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}
func Prepare(ctx context.Context, src, dest string) error {
	tmp := dest + ".tmp"
	_, err := Run(ctx, "ffmpeg", "-nostdin", "-v", "error", "-y", "-i", src, "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-f", "wav", tmp)
	if err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}
func Duration(ctx context.Context, path string) (float64, error) {
	b, err := Run(ctx, "ffprobe", "-v", "error", "-show_entries", "format=duration", "-of", "json", path)
	if err != nil {
		return 0, err
	}
	var p struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if json.Unmarshal(b, &p) != nil {
		return 0, fmt.Errorf("cannot read audio duration")
	}
	n, err := strconv.ParseFloat(p.Format.Duration, 64)
	if err != nil || n <= 0 || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, fmt.Errorf("invalid audio duration")
	}
	return n, nil
}
func Slice(ctx context.Context, src, dest string, start, duration float64) error {
	if start < 0 || duration <= 0 || math.IsNaN(start) || math.IsNaN(duration) || math.IsInf(start, 0) || math.IsInf(duration, 0) {
		return fmt.Errorf("invalid audio chunk boundaries")
	}
	tmp := dest + ".tmp"
	_, err := Run(ctx, "ffmpeg", "-nostdin", "-v", "error", "-y", "-ss", strconv.FormatFloat(start, 'f', 6, 64), "-i", src, "-t", strconv.FormatFloat(duration, 'f', 6, 64), "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-f", "wav", tmp)
	if err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}
