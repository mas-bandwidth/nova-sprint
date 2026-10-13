package store

// dropDefaultsOwed takes back the set-defaults owed flag the last advance wrote, so the
// advance is the one an older binary's clear made: its epoch record holds no flag
// (clear_owed_test.go olderBinaryClear).
func (m *Mem) dropDefaultsOwed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defaultsOwed = 0
}
