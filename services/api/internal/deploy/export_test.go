package deploy

import "time"

// SetPollEvery shortens the poll interval, for tests.
func SetPollEvery(s *Service, d time.Duration) { s.every = d }

// SetRetryAfter shortens the wait before a failed rollback is retried, for tests.
func SetRetryAfter(s *Service, d time.Duration) { s.retryAfter = d }

// SetWatchEvery shortens the interval of a watched stack's state reads, for tests.
func SetWatchEvery(s *Service, d time.Duration) { s.watchEvery = d }
