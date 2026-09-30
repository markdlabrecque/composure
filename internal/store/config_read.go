package store

import "context"

// ActiveConfig is the stored definition and active revision read from a site.
// Document remains raw so callers can report the validator's precise error.
type ActiveConfig struct {
	Document []byte
	Revision int64
}

// ReadActiveConfig applies the usual site identity, integrity and version
// guards, then returns the stored document without validating its contents.
func ReadActiveConfig(ctx context.Context, path string) (ActiveConfig, error) {
	opened, active, err := open(ctx, path, false, true)
	if err != nil {
		return ActiveConfig{}, err
	}
	if err := opened.Close(); err != nil {
		return ActiveConfig{}, err
	}
	return active, nil
}
