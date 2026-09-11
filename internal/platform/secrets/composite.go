package secrets

import "context"

// CompositeProvider resolves a reference by trying each provider in order and
// returning the first success.
type CompositeProvider struct {
	providers []Provider
}

// NewCompositeProvider builds a CompositeProvider over the given providers.
func NewCompositeProvider(providers ...Provider) *CompositeProvider {
	kept := providers[:0]
	for _, provider := range providers {
		if provider != nil {
			kept = append(kept, provider)
		}
	}
	return &CompositeProvider{providers: kept}
}

// Resolve tries every provider in order; the first success wins.
func (c *CompositeProvider) Resolve(ctx context.Context, ref string) ([]byte, error) {
	var lastErr error
	for _, provider := range c.providers {
		value, err := provider.Resolve(ctx, ref)
		if err == nil {
			return value, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = ErrNotFound
	}
	return nil, lastErr
}
