package translator

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/assanoff/skit/dim"
	"github.com/assanoff/skit/i18n"
)

// Resource returns a dim factory that builds an i18n.Translator from the
// message files in fsys. It holds no external resource, so the cleanup is nil.
func Resource(fsys fs.FS, cfg i18n.Config) func(ctx context.Context) (*i18n.Translator, dim.CleanupFunc, error) {
	return func(context.Context) (*i18n.Translator, dim.CleanupFunc, error) {
		tr, err := i18n.New(fsys, cfg)
		if err != nil {
			return nil, nil, fmt.Errorf("translator: %w", err)
		}
		return tr, nil, nil
	}
}
