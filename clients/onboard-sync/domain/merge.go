package domain

// HigherVersionWins resolves two operations for the same voyage: the higher
// Version wins; a tie keeps current (idempotent — re-applying the same edit
// changes nothing). The voyage service applies the identical rule server-side in
// UpdateVoyage (D-016), so a clock is never involved in conflict resolution.
func HigherVersionWins(current, incoming Operation) Operation {
	if incoming.Version > current.Version {
		return incoming
	}
	return current
}
