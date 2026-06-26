package auditlog

import (
	"context"
	"fmt"
)

// CompactOptions controls how a model's version history is thinned. The first
// (baseline) and last (current) versions are always kept, so diffs and the
// current snapshot remain available.
type CompactOptions struct {
	// Factor keeps roughly every Factor-th middle version (Factor >= 2). When
	// MaxVersions is also set, the cap takes precedence. Factor < 2 disables
	// factor thinning (only MaxVersions, if set, thins).
	Factor int
	// KeepRecent always retains this many most-recent versions untouched.
	KeepRecent int
	// MaxVersions, when > 0, caps the total kept versions: the middle is
	// downsampled evenly so no more than MaxVersions remain.
	MaxVersions int
	// MinVersions skips compaction entirely when a model has this many versions or
	// fewer. Defaults to 2 (nothing below first+last is ever thinned).
	MinVersions int
}

// Compact thins the stored version history of one model per opts and returns how
// many versions were deleted. The first and last versions are never removed.
func (c *Core) Compact(ctx context.Context, modelType, modelID string, opts CompactOptions) (int, error) {
	vers, err := c.store.Versions(ctx, modelType, modelID)
	if err != nil {
		return 0, fmt.Errorf("auditlog: compact %s/%s: %w", modelType, modelID, err)
	}

	del := versionsToDelete(vers, opts)
	if len(del) == 0 {
		return 0, nil
	}
	n, err := c.store.DeleteVersions(ctx, modelType, modelID, del)
	if err != nil {
		return 0, fmt.Errorf("auditlog: compact %s/%s: %w", modelType, modelID, err)
	}
	return n, nil
}

// versionsToDelete computes which versions (from the ascending list) to remove.
// It is pure for easy testing.
func versionsToDelete(vers []int, opts CompactOptions) []int {
	n := len(vers)
	minV := max(opts.MinVersions, 2)
	if n <= minV {
		return nil
	}

	keep := make([]bool, n)
	keep[0] = true   // first (baseline)
	keep[n-1] = true // last (current)

	// Always keep the most-recent KeepRecent versions.
	if opts.KeepRecent > 0 {
		start := max(n-opts.KeepRecent, 0)
		for i := start; i < n; i++ {
			keep[i] = true
		}
	}

	// Middle window that may be thinned: (0, recentStart).
	recentStart := n - 1
	if opts.KeepRecent > 0 && n-opts.KeepRecent < recentStart {
		recentStart = n - opts.KeepRecent
	}

	switch {
	case opts.MaxVersions > 0:
		// Cap mode: downsample the middle evenly to fit the budget.
		protected := countKept(keep)
		budget := opts.MaxVersions - protected
		keepEvenly(keep, 1, recentStart, budget)
	case opts.Factor >= 2:
		for i := 1; i < recentStart; i++ {
			if i%opts.Factor == 0 {
				keep[i] = true
			}
		}
	default:
		// No factor and no cap: keep everything (nothing to thin).
		for i := 1; i < recentStart; i++ {
			keep[i] = true
		}
	}

	var del []int
	for i, v := range vers {
		if !keep[i] {
			del = append(del, v)
		}
	}
	return del
}

func countKept(keep []bool) int {
	n := 0
	for _, k := range keep {
		if k {
			n++
		}
	}
	return n
}

// keepEvenly marks up to budget indices in [lo, hi) as kept, spaced evenly.
func keepEvenly(keep []bool, lo, hi, budget int) {
	span := hi - lo
	if span <= 0 || budget <= 0 {
		return
	}
	if budget >= span {
		for i := lo; i < hi; i++ {
			keep[i] = true
		}
		return
	}
	// Pick budget points spread across [lo, hi).
	for j := range budget {
		idx := lo + (j*span)/budget
		keep[idx] = true
	}
}

// CompactBatchOptions configures one batch of compaction across many models.
type CompactBatchOptions struct {
	// Threshold: only models with more than this many stored versions are
	// candidates.
	Threshold int
	// Limit caps how many models this call processes — the "portion". Default 100.
	Limit int
	// Compact options applied to each model.
	Compact CompactOptions
}

// CompactResult reports what a CompactBatch did.
type CompactResult struct {
	Models  int // models that had versions removed
	Deleted int // total versions removed
}

// CompactBatch compacts one portion of the models whose version count exceeds the
// threshold (most-versioned first), up to Limit models. It is the single reusable
// entry point for scheduled (NewSweeper), command-line, or API-triggered runs —
// call it repeatedly until Result.Models is 0 to drain a large backlog gently.
func (c *Core) CompactBatch(ctx context.Context, opts CompactBatchOptions) (CompactResult, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	refs, err := c.store.OverThreshold(ctx, opts.Threshold, limit)
	if err != nil {
		return CompactResult{}, fmt.Errorf("auditlog: compact batch: %w", err)
	}
	var res CompactResult
	for _, ref := range refs {
		n, err := c.Compact(ctx, ref.ModelType, ref.ModelID, opts.Compact)
		if err != nil {
			return res, err
		}
		if n > 0 {
			res.Models++
			res.Deleted += n
		}
	}
	return res, nil
}

// Scheduling is intentionally not built in: CompactBatch is the API, and the
// skit worker package runs it on whatever cadence you choose. Wire it once:
//
//	tick := func(ctx context.Context) error {
//	    _, err := core.CompactBatch(ctx, auditlog.CompactBatchOptions{
//	        Threshold: 100, Limit: 50,
//	        Compact:   auditlog.CompactOptions{Factor: 4, KeepRecent: 20},
//	    })
//	    return err
//	}
//	group.Add(worker.NewLoop(log.Slog(), worker.LoopConfig{
//	    Name: "auditlog-compaction", Interval: time.Hour,
//	}, tick))
//
// The same CompactBatch also backs an on-demand admin endpoint or a CLI command.
