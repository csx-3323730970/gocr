package job

import (
	"context"

	"github.com/csx-3323730970/gocr/internal/pipeline"
	"github.com/csx-3323730970/gocr/internal/report"
	"github.com/csx-3323730970/gocr/internal/store"
)

// PipelineProcessor runs the review pipeline on a job's stored diff. Serve
// mode is diff-only: server-side tools are disabled because clients do not
// control the filesystem.
type PipelineProcessor struct {
	Pipeline *pipeline.Pipeline
}

func (p *PipelineProcessor) Process(ctx context.Context, j store.Job) ([]report.Finding, error) {
	res, err := p.Pipeline.ReviewPatch(ctx, "", []byte(j.Diff))
	if err != nil {
		return nil, err
	}
	return res.Findings, nil
}
