package provider

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/assanoff/skit/dim"
	"github.com/assanoff/skit/i18n"
)

// Translator returns a dim factory that builds an i18n.Translator from the
// message files in fsys. It is language-agnostic: the caller supplies the
// default language and the message files (typically an embed.FS of the app's
// own translations) instead of the provider hard-coding a fixed set of
// languages.
//
//	//go:embed locales/*.json
//	var localesFS embed.FS
//
//	c.Translator, cleanup = dim.NewResource("Translator",
//		provider.Translator("ru", localesFS, "locales/ru.json", "locales/kk.json"))
//
// It holds no external resource, so the cleanup is nil.
func Translator(defaultLang string, fsys fs.FS, files ...string) func(ctx context.Context) (*i18n.Translator, dim.CleanupFunc, error) {
	return func(ctx context.Context) (*i18n.Translator, dim.CleanupFunc, error) {
		tr, err := i18n.New(defaultLang, fsys, files...)
		if err != nil {
			return nil, nil, fmt.Errorf("provider: init translator: %w", err)
		}
		return tr, nil, nil
	}
}
