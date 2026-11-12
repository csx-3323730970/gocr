package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/csx-3323730970/gocr/internal/config"
	"github.com/csx-3323730970/gocr/internal/llm"
	"github.com/csx-3323730970/gocr/internal/pipeline"
)

const promptVersion = "gocr-prompt-v1"

type evalCase struct {
	Name string
	Diff []byte
	Gold Gold
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "evals:", err)
		os.Exit(1)
	}
}

func run() error {
	casesDir := flag.String("cases", "evals/cases", "directory containing case folders")
	baselinePath := flag.String("baseline", "evals/baseline.json", "baseline metrics file")
	update := flag.Bool("update", false, "write current metrics to the baseline file")
	cfgPath := flag.String("config", "", "path to .gocr.yaml")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	cases, err := loadCases(*casesDir)
	if err != nil {
		return err
	}
	if len(cases) == 0 {
		return errors.New("no eval cases found")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	baseProvider := llm.NewOpenAI(cfg.Provider.BaseURL, cfg.Provider.APIKey, cfg.Provider.Model)
	provider := llm.NewRetryProvider(baseProvider, llm.RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   500 * time.Millisecond,
	})
	pipe := pipeline.New(provider, pipeline.Options{
		Concurrency:       cfg.Review.Concurrency,
		MaxRounds:         cfg.Review.MaxRounds,
		TokenBudget:       cfg.Review.TokenBudget,
		MaxFileBytes:      cfg.Review.MaxFileBytes,
		MaxFilesPerBundle: cfg.Review.BundleSize,
	})

	var results []CaseMetrics
	for _, c := range cases {
		start := time.Now()
		res, runErr := pipe.ReviewPatch(ctx, "", c.Diff)
		latency := time.Since(start).Milliseconds()

		if runErr != nil {
			results = append(results, CaseMetrics{
				Name:      c.Name,
				Gold:      len(c.Gold.Findings),
				LatencyMS: latency,
				Error:     runErr.Error(),
			})
			fmt.Printf("! %s: %v\n", c.Name, runErr)
			continue
		}

		matched, anchorOK := MatchFindings(res.Findings, c.Gold.Findings)
		cm := CaseMetrics{
			Name:      c.Name,
			Gold:      len(c.Gold.Findings),
			Predicted: len(res.Findings),
			Matched:   matched,
			AnchorOK:  anchorOK,
			Precision: ratio(matched, len(res.Findings)),
			Recall:    ratio(matched, len(c.Gold.Findings)),
			TokensIn:  res.Usage.InputTokens,
			TokensOut: res.Usage.OutputTokens,
			LatencyMS: latency,
		}
		if cm.Precision+cm.Recall > 0 {
			cm.F1 = 2 * cm.Precision * cm.Recall / (cm.Precision + cm.Recall)
		}
		results = append(results, cm)
		fmt.Printf("· %s: predicted=%d gold=%d matched=%d\n", c.Name, cm.Predicted, cm.Gold, cm.Matched)
	}

	m := Aggregate(results, cfg.Provider.Model, promptVersion, time.Now().UTC().Format(time.RFC3339))
	printMetricsTable(m)

	if *update {
		if err := writeBaseline(*baselinePath, m); err != nil {
			return err
		}
		fmt.Printf("\nbaseline written to %s\n", *baselinePath)
	} else if base, readErr := readBaseline(*baselinePath); readErr == nil {
		printBaselineDiff(m, base)
	}

	if m.Errors > 0 {
		return fmt.Errorf("%d of %d cases failed", m.Errors, m.Cases)
	}
	return nil
}

func loadCases(dir string) ([]evalCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var cases []evalCase
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		base := filepath.Join(dir, e.Name())
		diff, err := os.ReadFile(filepath.Join(base, "diff.patch"))
		if err != nil {
			return nil, fmt.Errorf("case %s: %w", e.Name(), err)
		}
		raw, err := os.ReadFile(filepath.Join(base, "gold.json"))
		if err != nil {
			return nil, fmt.Errorf("case %s: %w", e.Name(), err)
		}
		var gold Gold
		if err := json.Unmarshal(raw, &gold); err != nil {
			return nil, fmt.Errorf("case %s: %w", e.Name(), err)
		}
		cases = append(cases, evalCase{Name: e.Name(), Diff: diff, Gold: gold})
	}

	sort.Slice(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	return cases, nil
}

func printMetricsTable(m Metrics) {
	fmt.Println()
	fmt.Println("| Metric | Value |")
	fmt.Println("|---|---|")
	fmt.Printf("| cases | %d (errors %d) |\n", m.Cases, m.Errors)
	fmt.Printf("| gold / predicted / matched | %d / %d / %d |\n", m.Gold, m.Predicted, m.Matched)
	fmt.Printf("| precision | %.3f |\n", m.Precision)
	fmt.Printf("| recall | %.3f |\n", m.Recall)
	fmt.Printf("| F1 | %.3f |\n", m.F1)
	fmt.Printf("| anchor accuracy | %.3f |\n", m.AnchorAccuracy)
	fmt.Printf("| tokens in/out | %d / %d |\n", m.TokensIn, m.TokensOut)
	fmt.Printf("| P95 latency | %d ms |\n", m.P95LatencyMS)
	fmt.Printf("| model | %s |\n", m.Model)
	fmt.Printf("| prompt version | %s |\n", m.PromptVersion)
}

func printBaselineDiff(cur, base Metrics) {
	if base.GeneratedAt == "" {
		return
	}
	fmt.Printf("\nBaseline from %s (%s)\n\n", base.GeneratedAt, base.Model)
	fmt.Println("| Metric | Current | Baseline | Delta |")
	fmt.Println("|---|---|---|---|")
	rows := []struct {
		name       string
		cur, base  float64
		percentage bool
	}{
		{"precision", cur.Precision, base.Precision, true},
		{"recall", cur.Recall, base.Recall, true},
		{"F1", cur.F1, base.F1, true},
		{"anchor accuracy", cur.AnchorAccuracy, base.AnchorAccuracy, true},
	}
	for _, r := range rows {
		if r.percentage {
			fmt.Printf("| %s | %.3f | %.3f | %+.3f |\n", r.name, r.cur, r.base, r.cur-r.base)
		}
	}
}

func readBaseline(path string) (Metrics, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Metrics{}, err
	}
	var m Metrics
	if err := json.Unmarshal(raw, &m); err != nil {
		return Metrics{}, err
	}
	return m, nil
}

func writeBaseline(path string, m Metrics) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}
