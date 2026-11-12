package cli

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/csx-3323730970/gocr/internal/api"
	"github.com/csx-3323730970/gocr/internal/config"
	"github.com/csx-3323730970/gocr/internal/job"
	"github.com/csx-3323730970/gocr/internal/llm"
	"github.com/csx-3323730970/gocr/internal/obs"
	"github.com/csx-3323730970/gocr/internal/pipeline"
	mysqlstore "github.com/csx-3323730970/gocr/internal/store/mysql"
)

func newServeCmd() *cobra.Command {
	var (
		cfgPath string
		addr    string
		dsn     string
	)

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the async review service (HTTP + workers)",
		RunE: func(_ *cobra.Command, _ []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if addr != "" {
				cfg.Server.Addr = addr
			}
			if dsn != "" {
				cfg.Server.DSN = dsn
			}
			if cfg.Server.DSN == "" {
				return errors.New("mysql DSN is required: set GOCR_DSN or pass --dsn")
			}

			log := obs.NewLogger()
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			st, err := mysqlstore.Open(ctx, cfg.Server.DSN)
			if err != nil {
				return err
			}
			defer st.Close()

			if err := st.Migrate(ctx); err != nil {
				return err
			}

			provider := llm.Provider(llm.NewOpenAI(cfg.Provider.BaseURL, cfg.Provider.APIKey, cfg.Provider.Model))
			provider = llm.NewRateLimitedProvider(provider, job.NewLimiter(cfg.Provider.RPM))
			provider = llm.NewCachingProvider(provider, st.Cache(), cfg.Provider.BaseURL+"|"+cfg.Provider.Model)
			provider = llm.NewRetryProvider(provider, llm.RetryPolicy{
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
			proc := &job.PipelineProcessor{Pipeline: pipe}

			workerCount := cfg.Server.Workers
			if workerCount <= 0 {
				workerCount = 1
			}
			var wg sync.WaitGroup
			for i := 0; i < workerCount; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					job.NewWorker(st, proc, log).Run(ctx)
				}()
			}
			log.Info("workers started", "count", workerCount)

			srv := api.NewServer(cfg.Server.Addr, st, log, cfg.Server.MaxQueue)
			errCh := make(chan error, 1)
			go func() { errCh <- srv.ListenAndServe() }()

			select {
			case err := <-errCh:
				stop()
				wg.Wait()
				return err
			case <-ctx.Done():
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				shutdownErr := srv.Shutdown(shutdownCtx)

				done := make(chan struct{})
				go func() {
					wg.Wait()
					close(done)
				}()
				select {
				case <-done:
				case <-time.After(10 * time.Second):
					log.Warn("workers did not stop within timeout")
				}
				return shutdownErr
			}
		},
	}

	cmd.Flags().StringVar(&cfgPath, "config", "", "path to .gocr.yaml")
	cmd.Flags().StringVar(&addr, "addr", "", "HTTP listen address")
	cmd.Flags().StringVar(&dsn, "dsn", "", "MySQL DSN")
	return cmd
}
