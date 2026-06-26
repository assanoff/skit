package translation

import "errors"

// Config holds configuration for the Translator.
type Config struct {
	// Store is the translation storage backend (e.g. the postgres subpackage).
	Store Store

	// DefaultLanguage is the language the canonical content is authored in.
	// Translating a model to the default language is a no-op.
	DefaultLanguage Language

	// SupportedLangs is the set of languages the application serves. It must
	// include DefaultLanguage.
	SupportedLangs []Language
}

// Validate checks that the configuration is usable.
func (c *Config) Validate() error {
	if c.Store == nil {
		return errors.New("Store must be provided")
	}

	if c.DefaultLanguage.Code == "" {
		return errors.New("DefaultLanguage must be set")
	}

	if len(c.SupportedLangs) == 0 {
		return errors.New("SupportedLangs must contain at least one language")
	}

	for _, lang := range c.SupportedLangs {
		if lang.Code == c.DefaultLanguage.Code {
			return nil
		}
	}
	return errors.New("DefaultLanguage must be in SupportedLangs")
}
