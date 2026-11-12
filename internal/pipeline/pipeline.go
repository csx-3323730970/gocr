// Package pipeline orchestrates one full review.
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"github.com/csx-3323730970/gocr/internal/agent"
	"github.com/csx-3323730970/gocr/internal/agent/tools"
	"github.com/csx-3323730970/gocr/internal/anchor"
	"github.com/csx-3323730970/gocr/internal/bundle"
	"github.com/csx-3323730970/gocr/internal/llm"
	"github.com/csx-3323730970/gocr/internal/obs"
	"github.com/csx-3323730970/gocr/internal/report"
	"github.com/csx-3323730970/gocr/internal/rules"
	"github.com/csx-3323730970/gocr/internal/selector"
	"github.com/csx-3323730970/gocr/internal/source"
)

type Input struct {
	RepoPath string
	Base     string
	Head     string
}

type Options struct {
	Concurrency       int
	MaxRounds         int
	TokenBudget       int
	MaxFileBytes      int64
	MaxFilesPerBundle int
	MaxDiffBytes      int
	IgnoreGlobs       []string
	Allowlist         []string
	ExtraRules        []rules.Layer
	ToolsRoot         string // empty disables tools (diff-only mode)
}

type Result struct {
	Findings      []report.Finding
	Usage         llm.Usage
	Files         int
	Bundles       int
	FailedBundles int
}

type Pipeline struct {
	Provider llm.Provider
	Options  Options
}

func New(provider llm.Provider, opts Options) *Pipeline {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 4
	}
	if opts.MaxRounds <= 0 {
		opts.MaxRounds = 4
	}
	if opts.MaxFileBytes <= 0 {
		opts.MaxFileBytes = 1 << 20
	}
	if opts.MaxFilesPerBundle <= 0 {
		opts.MaxFilesPerBundle = 5
	}
	if opts.MaxDiffBytes <= 0 {
		opts.MaxDiffBytes = 60 << 10
	}
	return &Pipeline{Provider: provider, Options: opts}
}

// Review runs the pipeline against a real git repository.
func (p *Pipeline) Review(ctx context.Context, in Input) (Result, error) {
	patch, err := source.NewGit(in.RepoPath).Diff(ctx, in.Base, in.Head)
	if err != nil {
		return Result{}, err
	}
	return p.ReviewPatch(ctx, in.RepoPath, patch)
}

// Preview lists files and bundles without calling the model.
func (p *Pipeline) Preview(ctx context.Context, in Input) ([]string, int, error) {
	files, err := p.SelectFiles(ctx, in)
	if err != nil {
		return nil, 0, err
	}
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	return paths, len(bundle.Group(files, p.Options.MaxFilesPerBundle)), nil
}

func (p *Pipeline) SelectFiles(ctx context.Context, in Input) ([]source.FileDiff, error) {
	patch, err := source.NewGit(in.RepoPath).Diff(ctx, in.Base, in.Head)
	if err != nil {
		return nil, err
	}
	return p.selectFiles(patch)
}

// ReviewPatch runs the pipeline against an already-fetched unified diff.
func (p *Pipeline) ReviewPatch(ctx context.Context, repoRoot string, patch []byte) (Result, error) {
	files, err := p.selectFiles(patch)
	if err != nil {
		return Result{}, err
	}
	res := Result{Files: len(files)}
	if len(files) == 0 {
		return res, nil
	}

	ruleSet, err := rules.Build(repoRoot, p.Options.ExtraRules)
	if err != nil {
		return res, err
	}

	byPath := make(map[string]source.FileDiff, len(files))
	for _, f := range files {
		byPath[f.Path] = f
	}

	bundles := bundle.Group(files, p.Options.MaxFilesPerBundle)
	res.Bundles = len(bundles)

	var mu sync.Mutex
	findings, err := bundle.Run(ctx, bundles, p.Options.Concurrency, func(ctx context.Context, b bundle.Bundle) ([]report.Finding, error) {
		bundleFiles := make([]source.FileDiff, 0, len(b.Paths))
		ruleDocs := make([]string, 0, len(b.Paths))
		for _, path := range b.Paths {
			fd, ok := byPath[path]
			if !ok {
				continue
			}
			bundleFiles = append(bundleFiles, fd)
			ruleDocs = append(ruleDocs, ruleSet.For(path))
		}
		if len(bundleFiles) == 0 {
			return nil, nil
		}

		diffText, _ := source.RenderDiff(bundleFiles, p.Options.MaxDiffBytes)
		userPrompt := agent.UserPrompt(strings.Join(uniqueStrings(ruleDocs), "\n\n"), diffText)

		ag := &agent.Agent{
			Provider:    p.Provider,
			Tools:       p.tools(),
			MaxRounds:   p.Options.MaxRounds,
			TokenBudget: p.Options.TokenBudget,
		}
		agentRes, agentErr := ag.Review(ctx, agent.SystemPrompt(), userPrompt)
		if agentErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			slog.Default().Warn("bundle review failed", "paths", b.Paths, "err", agentErr)
			mu.Lock()
			res.FailedBundles++
			mu.Unlock()
			return nil, nil
		}

		mu.Lock()
		res.Usage.InputTokens += agentRes.Usage.InputTokens
		res.Usage.OutputTokens += agentRes.Usage.OutputTokens
		mu.Unlock()

		var out []report.Finding
		for _, f := range agentRes.Findings {
			fd, path, ok := matchFile(byPath, f.File)
			if !ok {
				continue
			}
			ar := anchor.Anchor(fd, f.Line, f.Snippet)
			obs.AnchorMethodTotal.WithLabelValues(string(ar.Method)).Inc()
			if ar.Method == anchor.MethodDropped {
				continue
			}
			f.File = path
			f.Line = ar.Line
			f.Anchor = report.Anchor{
				Confidence: ar.Confidence,
				Method:     string(ar.Method),
				Original:   ar.Original,
			}
			out = append(out, f)
		}
		return out, nil
	})
	if err != nil {
		return res, err
	}

	res.Findings = dedupe(findings)
	report.Sort(res.Findings)
	return res, nil
}

func (p *Pipeline) selectFiles(patch []byte) ([]source.FileDiff, error) {
	all, err := source.ParseFileDiffs(patch)
	if err != nil {
		return nil, err
	}

	ignore := append(append([]string{}, selector.DefaultIgnoreGlobs...), p.Options.IgnoreGlobs...)
	filter := selector.Filter{
		Allowlist:    p.Options.Allowlist,
		IgnoreGlobs:  ignore,
		MaxFileBytes: p.Options.MaxFileBytes,
	}

	var out []source.FileDiff
	for _, fd := range all {
		if fd.IsBinary || fd.Status == "deleted" {
			continue
		}
		if len(fd.CommentableLines()) == 0 {
			continue
		}
		if !filter.Keep(fd.Path, int64(fd.ByteSize())) {
			continue
		}
		out = append(out, fd)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (p *Pipeline) tools() []agent.Tool {
	if p.Options.ToolsRoot == "" {
		return nil
	}
	root := p.Options.ToolsRoot
	return []agent.Tool{
		&tools.ReadFile{Root: root},
		&tools.Grep{Root: root},
		&tools.HunkContext{Root: root},
	}
}

func matchFile(byPath map[string]source.FileDiff, name string) (source.FileDiff, string, bool) {
	name = strings.TrimSpace(name)
	if fd, ok := byPath[name]; ok {
		return fd, name, true
	}

	alt := strings.TrimPrefix(name, "./")
	if fd, ok := byPath[alt]; ok {
		return fd, alt, true
	}

	alt = strings.TrimPrefix(strings.TrimPrefix(alt, "a/"), "b/")
	if fd, ok := byPath[alt]; ok {
		return fd, alt, true
	}
	return source.FileDiff{}, "", false
}

func dedupe(findings []report.Finding) []report.Finding {
	seen := make(map[string]struct{}, len(findings))
	out := make([]report.Finding, 0, len(findings))
	for _, f := range findings {
		key := fmt.Sprintf("%s\x00%d\x00%s", f.File, f.Line, f.Message)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, f)
	}
	return out
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
