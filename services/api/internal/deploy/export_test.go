package deploy

import "time"

// SetPollEvery shortens the poll interval, for tests.
func SetPollEvery(s *Service, d time.Duration) { s.every = d }
