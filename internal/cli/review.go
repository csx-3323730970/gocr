package cli

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/csx-3323730970/gocr/internal/config"
	"github.com/csx-3323730970/gocr/internal/llm"
	"github.com/csx-3323730970/gocr/internal/pipeline"
	"github.com/csx-3323730970/gocr/internal/report"
	"github.com/csx-3323730970/gocr/internal/rules"
)

// ErrBlockingFindings signals a non-zero exit code for CI. main() suppresses
// the error line and exits 1.
var ErrBlockingFindings = errors.New("blocking findings found")

func newReviewCmd() *cobra.Command {
	var (
		cfgPath   string
		repoPath  string
		baseRef   string
		headRef   string
		ruleFiles []string
		dryRun    bool
		jsonOut   bool
	)

	cmd := &cobra.Command{
		Use:   "review",
		Short: "Review a git diff locally (stateless, no database)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			extraRules, err := rules.FromFiles(ruleFiles)
			if err != nil {
				return err
			}

			opts := pipeline.Options{
				Concurrency:       cfg.Review.Concurrency,
				MaxRounds:         cfg.Review.MaxRounds,
				TokenBudget:       cfg.Review.TokenBudget,
				MaxFileBytes:      cfg.Review.MaxFileBytes,
				MaxFilesPerBundle: cfg.Review.BundleSize,
				ExtraRules:        extraRules,
				ToolsRoot:         repoPath,
			}
			in := pipeline.Input{RepoPath: repoPath, Base: baseRef, Head: headRef}

			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			if dryRun {
				files, bundles, err := pipeline.New(nil, opts).Preview(ctx, in)
				if err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				for _, f := range files {
					fmt.Fprintln(out, f)
				}
				fmt.Fprintf(out, "%d files -> %d bundles\n", len(files), bundles)
				return nil
			}

			baseProvider := llm.NewOpenAI(cfg.Provider.BaseURL, cfg.Provider.APIKey, cfg.Provider.Model)
			provider := llm.NewRetryProvider(baseProvider, llm.RetryPolicy{
				MaxAttempts: 3,
				BaseDelay:   500 * time.Millisecond,
			})

			res, err := pipeline.New(provider, opts).Review(ctx, in)
			if err != nil {
				return err
			}
			if res.FailedBundles > 0 {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"warning: %d of %d bundle(s) failed; results are partial\n",
					res.FailedBundles, res.Bundles)
			}

			out := cmd.OutOrStdout()
			if jsonOut {
				if err := report.RenderJSON(out, res.Findings); err != nil {
					return err
				}
			} else {
				if err := report.RenderTerminal(out, res.Findings); err != nil {
					return err
				}
				fmt.Fprintf(out, "reviewed %d files in %d bundles · tokens in/out %d/%d\n",
					res.Files, res.Bundles, res.Usage.InputTokens, res.Usage.OutputTokens)
			}

			for _, f := range res.Findings {
				if f.Severity == report.SeverityBlocker || f.Severity == report.SeverityMajor {
					return ErrBlockingFindings
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cfgPath, "config", "", "path to .gocr.yaml")
	cmd.Flags().StringVar(&repoPath, "repo", ".", "git repository path")
	cmd.Flags().StringVar(&baseRef, "base", "HEAD~1", "base ref")
	cmd.Flags().StringVar(&headRef, "head", "HEAD", "head ref")
	cmd.Flags().StringSliceVar(&ruleFiles, "rule", nil, "extra rule file (repeatable)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list files to review without calling the model")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "print findings as JSON")
	return cmd
}
