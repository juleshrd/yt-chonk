package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"yt-chonk/internal/store"
)

type Config struct {
	Version    int    `json:"version"`
	Backend    string `json:"backend"`
	Format     string `json:"format"`
	Model      string `json:"model"`
	Language   string `json:"language"`
	LocalModel string `json:"local_model"`
}

func dirs() (string, string, error) {
	c, e := os.UserConfigDir()
	if e != nil {
		return "", "", e
	}
	k, e := os.UserCacheDir()
	return filepath.Join(c, "yt-chonk"), filepath.Join(k, "yt-chonk"), e
}
func loadConfig(dir string) (Config, error) {
	c := Config{Version: 1, Format: "txt"}
	b, e := os.ReadFile(filepath.Join(dir, "config.json"))
	if os.IsNotExist(e) {
		return c, nil
	}
	if e != nil {
		return c, e
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return c, fmt.Errorf("Could not read configuration: %w", e)
	}
	if c.Version != 1 {
		return c, errors.New("Unsupported configuration version")
	}
	return c, nil
}
func key(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if s := os.Getenv("MISTRAL_API_KEY"); s != "" {
		return s
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.CommandContext(ctx, "security", "find-generic-password", "-s", "yt-chonk", "-a", "mistral", "-w")
	} else {
		cmd = exec.CommandContext(ctx, "secret-tool", "lookup", "service", "yt-chonk", "account", "mistral")
	}
	b, e := cmd.Output()
	if e != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
func saveKey(ctx context.Context, s string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.CommandContext(ctx, "security", "-i")
		q := func(s string) string {
			return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", "", "\r", "").Replace(s) + `"`
		}
		cmd.Stdin = strings.NewReader("add-generic-password -U -s yt-chonk -a mistral -w " + q(s) + "\n")
	} else {
		cmd = exec.CommandContext(ctx, "secret-tool", "store", "--label=yt-chonk Mistral", "service", "yt-chonk", "account", "mistral")
		cmd.Stdin = strings.NewReader(s)
	}
	if cmd.Run() != nil {
		return errors.New("Keychain unavailable. Set MISTRAL_API_KEY instead")
	}
	return nil
}
func setup(ctx context.Context, dir string, c Config, in io.Reader, out io.Writer) error {
	r := bufio.NewReader(in)
	readLine := func() string {
		if ctx.Err() != nil {
			return ""
		}
		ch := make(chan string, 1)
		go func() { s, _ := r.ReadString('\n'); ch <- s }()
		select {
		case s := <-ch:
			return s
		case <-ctx.Done():
			return ""
		}
	}
	ask := func(label, def string) string {
		fmt.Fprintf(out, "%s [%s]: ", label, def)
		s := readLine()
		s = strings.TrimSpace(s)
		if s == "" {
			return def
		}
		return s
	}
	c.Backend = ask("Backend (mistral/local)", c.Backend)
	if c.Backend != "mistral" && c.Backend != "local" {
		return errors.New("Choose mistral or local")
	}
	c.Format = ask("Export (txt/md/json)", c.Format)
	if c.Format != "txt" && c.Format != "md" && c.Format != "json" {
		return errors.New("Choose txt, md or json")
	}
	if c.Backend == "local" {
		c.Model = "small"
		c.LocalModel = ask("Path to the multilingual whisper.cpp small model", c.LocalModel)
		fmt.Fprintln(out, "Install whisper.cpp and download ggml-small.bin from the official models: https://github.com/ggml-org/whisper.cpp/tree/master/models")
	} else {
		c.Model = "voxtral-mini-transcribe-3"
		if ask("Save an API key in the system keychain? (yes/no)", "no") == "yes" {
			f, ok := in.(*os.File)
			if !ok {
				return errors.New("Hidden input requires a terminal. Set MISTRAL_API_KEY instead")
			}
			off := exec.Command("stty", "-echo")
			off.Stdin = f
			if off.Run() != nil {
				return errors.New("Hidden input unavailable. Set MISTRAL_API_KEY instead")
			}
			defer func() { on := exec.Command("stty", "echo"); on.Stdin = f; _ = on.Run() }()
			fmt.Fprint(out, "Mistral API key: ")
			s := readLine()
			fmt.Fprintln(out)
			if e := saveKey(ctx, strings.TrimSpace(s)); e != nil {
				return e
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	c.Version = 1
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	if e := store.WriteJSON(filepath.Join(dir, "config.json"), c); e != nil {
		return e
	}
	fmt.Fprintln(out, "Settings saved. Run yt-chonk doctor to check your dependencies.")
	return nil
}
