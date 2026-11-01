package tlsacme

// Clock is where a manager reads time.
type Clock = clock

// SetClock replaces the manager's clock; call it before Run.
func (m *Manager) SetClock(c Clock) { m.clock = c }
