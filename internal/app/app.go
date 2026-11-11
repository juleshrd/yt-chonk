package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"yt-chonk/internal/adapters"
	"yt-chonk/internal/core"
)

var Version = "0.1.1"

const help = `yt-chonk: transcribe YouTube audio in its original language

Commands:
  setup                  Compare model prices and save preferences
  doctor                 Check dependencies for your backend
  transcribe URL [--backend mistral|local] [--format txt|md|json]
    [--language CODE] [--model MODEL] [--output PATH] [--force] [--quiet]
  cache clean            Remove inactive cached jobs

TXT by default. Choose a backend with setup or --backend.
`

var videoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func identity(raw string) (string, string, error) {
	u, e := url.Parse(raw)
	if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Port() != "" {
		return "", "", errors.New("Invalid YouTube URL")
	}
	host := strings.ToLower(u.Hostname())
	id := ""
	switch host {
	case "youtu.be":
		id = strings.Trim(u.Path, "/")
	case "youtube.com", "www.youtube.com", "m.youtube.com", "music.youtube.com":
		if u.Path == "/watch" {
			id = u.Query().Get("v")
		} else {
			p := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(p) == 2 && (p[0] == "shorts" || p[0] == "embed" || p[0] == "live") {
				id = p[1]
			}
		}
	}
	if !videoID.MatchString(id) {
		return "", "", errors.New("Provide the URL of a single YouTube video")
	}
	return id, "https://www.youtube.com/watch?v=" + id, nil
}

type configError struct{ message string }

func (e configError) Error() string { return e.message }

type options struct {
	backend, model, language, format, output, localModel string
	force, quiet                                         bool
}

func Run(ctx context.Context, args []string, in io.Reader, out, diag io.Writer) int {
	fail := func(e error, code int) int {
		var ce configError
		if errors.As(e, &ce) {
			code = 2
		}
		fmt.Fprintln(diag, "Error:", e)
		if ctx.Err() != nil {
			return 130
		}
		return code
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(out, help)
		return 0
	}
	if args[0] == "--version" {
		fmt.Fprintln(out, "yt-chonk", Version)
		return 0
	}
	confDir, cacheDir, e := dirs()
	if e != nil {
		return fail(e, 2)
	}
	if args[0] == "cache" && len(args) == 2 && args[1] == "clean" {
		if e = clean(ctx, cacheDir); e != nil {
			return fail(e, 1)
		}
		return 0
	}
	c, e := loadConfig(confDir)
	if e != nil {
		return fail(e, 2)
	}
	switch args[0] {
	case "setup":
		if len(args) != 1 {
			return fail(errors.New("setup takes no arguments"), 2)
		}
		if e = setup(ctx, confDir, c, in, diag); e != nil {
			return fail(e, 2)
		}
		return 0
	case "doctor":
		if len(args) != 1 {
			return fail(errors.New("doctor uses the backend saved by setup; it takes no arguments"), 2)
		}
		if e = doctor(ctx, c, diag); e != nil {
			return fail(e, 2)
		}
		return 0
	case "transcribe":
	default:
		return fail(errors.New("Unknown command; run yt-chonk --help"), 2)
	}
	f := flag.NewFlagSet("transcribe", flag.ContinueOnError)
	f.SetOutput(diag)
	o := options{}
	f.StringVar(&o.backend, "backend", c.Backend, "Backend: mistral or local")
	f.StringVar(&o.model, "model", c.Model, "Model for the selected backend")
	f.StringVar(&o.language, "language", c.Language, "ISO 639-1 language code (auto-detect by default)")
	f.StringVar(&o.format, "format", c.Format, "txt, md or json")
	f.StringVar(&o.output, "output", "", "Output file")
	f.BoolVar(&o.force, "force", false, "Replace an existing output file")
	f.BoolVar(&o.quiet, "quiet", false, "Hide progress messages")
	rest := args[1:]
	raw := ""
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		raw = rest[0]
		rest = rest[1:]
	}
	if e = f.Parse(rest); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if raw == "" && f.NArg() == 1 {
		raw = f.Arg(0)
	} else if f.NArg() != 0 {
		return fail(errors.New("Expected a single URL"), 2)
	}
	id, canonical, e := identity(raw)
	if e != nil {
		return fail(e, 2)
	}
	if o.backend != "mistral" && o.backend != "local" {
		return fail(errors.New("Run yt-chonk setup or pass --backend mistral|local"), 2)
	}
	modelExplicit := false
	f.Visit(func(v *flag.Flag) {
		if v.Name == "model" {
			modelExplicit = true
		}
	})
	if !modelExplicit && o.backend != c.Backend {
		o.model = ""
	}
	if o.model == "" {
		if o.backend == "local" {
			o.model = "small"
		} else {
			o.model = "voxtral-mini-transcribe-3"
		}
	}
	if o.format != "txt" && o.format != "md" && o.format != "json" {
		return fail(errors.New("Expected format: txt, md or json"), 2)
	}
	if o.language != "" && !regexp.MustCompile(`^[a-z]{2}$`).MatchString(o.language) {
		return fail(errors.New("Use a lowercase ISO 639-1 language code, such as en or fr"), 2)
	}
	o.localModel = c.LocalModel
	if o.backend == "local" && o.model != "small" {
		o.localModel = o.model
	}
	path, e := transcribe(ctx, cacheDir, id, canonical, o, diag)
	if e != nil {
		return fail(e, 1)
	}
	fmt.Fprintln(out, path)
	return 0
}
func doctor(ctx context.Context, c Config, out io.Writer) error {
	if c.Backend != "local" && c.Backend != "mistral" {
		return errors.New("Run yt-chonk setup to choose a backend")
	}
	tools := []string{"yt-dlp", "ffmpeg", "ffprobe", "deno"}
	if c.Backend == "local" {
		tools = append(tools, "whisper-cli")
	}
	bad := false
	for _, name := range tools {
		p, e := exec.LookPath(name)
		if e != nil {
			fmt.Fprintf(out, "Missing: %s. Install it, then run yt-chonk doctor again.\n", name)
			bad = true
			continue
		}
		args := []string{"--version"}
		if name == "ffmpeg" || name == "ffprobe" {
			args = []string{"-version"}
		}
		if name == "whisper-cli" {
			args = []string{"--help"}
		}
		probe, cancel := context.WithTimeout(ctx, 5*time.Second)
		b, e := adapters.Probe(probe, name, args...)
		cancel()
		line := strings.SplitN(string(b), "\n", 2)[0]
		fmt.Fprintf(out, "%s: %s %s\n", name, p, line)
		if e != nil {
			fmt.Fprintf(out, "Could not check %s\n", name)
			bad = true
		}
	}
	fmt.Fprintln(out, "EJS must be included with yt-dlp. Doctor does not contact YouTube to check it.")
	if c.Backend == "local" {
		if c.LocalModel == "" {
			fmt.Fprintln(out, "Missing model: run yt-chonk setup and set the path to ggml-small.bin.")
			bad = true
		} else if _, e := os.Stat(c.LocalModel); e != nil {
			fmt.Fprintln(out, "Local model not found. Run yt-chonk setup to update its path.")
			bad = true
		} else {
			fmt.Fprintln(out, "Local model:", c.LocalModel, "; CPU supported, acceleration depends on your whisper.cpp build.")
		}
	} else if key(ctx) == "" {
		fmt.Fprintln(out, "Missing API key. Set MISTRAL_API_KEY or run yt-chonk setup.")
		bad = true
	}
	if bad {
		return errors.New("Missing dependencies or configuration; fix the items listed above")
	}
	return nil
}
func required(names ...string) error {
	for _, n := range names {
		if _, e := exec.LookPath(n); e != nil {
			return fmt.Errorf("%s not found. Install it, then try again", n)
		}
	}
	return nil
}
func destination(o options, v core.Video) (string, error) {
	p := o.output
	if p == "" {
		p = filename(v, o.format)
	}
	a, e := filepath.Abs(p)
	if e != nil {
		return "", e
	}
	if !o.force {
		if _, e = os.Lstat(a); e == nil {
			return "", errors.New("Output already exists. Choose another --output or use --force")
		} else if !os.IsNotExist(e) {
			return "", e
		}
	}
	return a, nil
}
