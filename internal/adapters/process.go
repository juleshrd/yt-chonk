package adapters

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

const maxOutput = 8 << 20

type boundedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := maxOutput - b.Len()
	if len(p) > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

// Run never includes child output or arguments in errors: both may contain secrets.
func Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return run(ctx, false, name, args...)
}

// Probe captures diagnostic output, including tools that print help to stderr.
// Call it only with fixed version/help arguments, never transcription input.
func Probe(ctx context.Context, name string, args ...string) ([]byte, error) {
	return run(ctx, true, name, args...)
}

func run(ctx context.Context, diagnostic bool, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 2 * time.Second
	var out boundedBuffer
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if diagnostic {
		cmd.Stderr = &out
	}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if _, ok := err.(*exec.Error); ok {
			return nil, fmt.Errorf("%s not found: install this dependency and check PATH", filepath.Base(name))
		}
		return nil, fmt.Errorf("%s failed: check the input and tool version", filepath.Base(name))
	}
	if out.overflow {
		return nil, fmt.Errorf("output from %s is too large", filepath.Base(name))
	}
	return out.Bytes(), nil
}
