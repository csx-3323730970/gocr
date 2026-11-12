package source

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// Git shells out to the git binary. This is the only package allowed to touch git,
// which keeps everything else mockable.
type Git struct {
	RepoPath string
	Timeout  time.Duration
	MaxBytes int64
}

func NewGit(repoPath string) *Git {
	return &Git{
		RepoPath: repoPath,
		Timeout:  30 * time.Second,
		MaxBytes: 32 << 20,
	}
}

// Diff returns the unified diff between the merge base of base and head.
// Three-dot semantics match pull-request review.
func (g *Git) Diff(ctx context.Context, base, head string) ([]byte, error) {
	if base == "" || head == "" {
		return nil, fmt.Errorf("git diff: base and head are required")
	}
	if strings.HasPrefix(base, "-") || strings.HasPrefix(head, "-") {
		return nil, fmt.Errorf("git diff: invalid ref")
	}

	timeout := g.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", g.RepoPath, "diff",
		"--no-color", "--find-renames", "--unified=3", base+"..."+head)

	limit := g.MaxBytes
	if limit <= 0 {
		limit = 32 << 20
	}
	stdout := &limitedBuffer{max: limit}
	var stderr bytes.Buffer
	cmd.Stdout = stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("git diff: timed out after %s", timeout)
	}
	if stdout.err != nil {
		return nil, stdout.err
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 500 {
			msg = msg[:500]
		}
		return nil, fmt.Errorf("git diff: %w: %s", err, msg)
	}
	return stdout.Bytes(), nil
}

type limitedBuffer struct {
	buf bytes.Buffer
	max int64
	err error
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	if int64(b.buf.Len()+len(p)) > b.max {
		b.err = ErrOutputTooLarge
		return 0, b.err
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) Bytes() []byte { return b.buf.Bytes() }

var _ io.Writer = (*limitedBuffer)(nil)
