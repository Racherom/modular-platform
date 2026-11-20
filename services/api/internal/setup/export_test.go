package setup

import (
	"context"
	"time"
)

// SetWait replaces the retry sleep, for tests.
func (s *Service) SetWait(f func(ctx context.Context, d time.Duration) error) { s.wait = f }
