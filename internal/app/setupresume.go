package app

import (
	"context"
	"errors"

	"github.com/F31/liteAIG/internal/controlplane/setup"
)

// resumableBootstrapper makes the one-time bootstrap retryable: when the
// underlying bootstrapper reports ErrAlreadyInitialized, it asks a half-state
// resetter to clear the previous interrupted wizard run and boots again. A
// failed setup therefore stays retryable from the Console instead of
// permanently locking the installation.
type resumableBootstrapper struct {
	bootstrap setup.Bootstrapper
	resetter  setup.HalfStateResetter
}

func (r resumableBootstrapper) Bootstrap(ctx context.Context, input setup.Input) (*setup.Result, error) {
	result, err := r.bootstrap.Bootstrap(ctx, input)
	if err == nil {
		return result, nil
	}
	if !errors.Is(err, setup.ErrAlreadyInitialized) {
		return nil, err
	}
	cleaned, resetErr := r.resetter.ResetHalfInitialized(ctx)
	if resetErr != nil {
		return nil, resetErr
	}
	if !cleaned {
		return nil, setup.ErrAlreadyInitialized
	}
	return r.bootstrap.Bootstrap(ctx, input)
}

var _ setup.Bootstrapper = resumableBootstrapper{}
