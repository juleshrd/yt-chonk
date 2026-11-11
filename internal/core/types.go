package core

import "context"

type Video struct {
	ID       string  `json:"id"`
	URL      string  `json:"url"`
	Title    *string `json:"title"`
	Channel  *string `json:"channel"`
	Duration float64 `json:"duration_seconds"`
}
type Segment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}
type Result struct {
	Text     string    `json:"text"`
	Segments []Segment `json:"segments"`
	Language *string   `json:"language"`
	Model    string    `json:"model"`
}
type Transcript struct {
	SchemaVersion int       `json:"schema_version"`
	Video         Video     `json:"video"`
	Backend       string    `json:"backend"`
	Model         string    `json:"model"`
	Language      *string   `json:"language"`
	Text          string    `json:"text"`
	Segments      []Segment `json:"segments"`
}
type Engine interface {
	Transcribe(context.Context, string, string, string) (Result, error)
}
