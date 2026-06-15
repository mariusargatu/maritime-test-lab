package domain_test

import (
	"testing"

	"pgregory.net/rapid"

	"maritime-test-lab/clients/onboard-sync/domain"
)

// HigherVersionWins is the conflict-resolution lattice (the same rule the server
// applies in UpdateVoyage, D-016). Property tests pin its invariants across the
// whole input space, beside the golden table in merge_test.go. Seeded, so any
// failure replays deterministically (CLAUDE.md's sanctioned randomness exception).

func op(version, fees int64) domain.Operation {
	return domain.Operation{ClientRequestID: "x", Version: version, FeesMinor: fees}
}

func TestHigherVersionWins_ResultIsTheMaxVersion(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cur := rapid.Int64Range(0, 1_000_000).Draw(t, "current")
		inc := rapid.Int64Range(0, 1_000_000).Draw(t, "incoming")

		got := domain.HigherVersionWins(op(cur, 100), op(inc, 200))

		max := cur
		if inc > max {
			max = inc
		}
		if got.Version != max {
			t.Fatalf("winner version = %d, want max(%d,%d) = %d", got.Version, cur, inc, max)
		}
	})
}

func TestHigherVersionWins_Idempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		v := rapid.Int64Range(0, 1_000_000).Draw(t, "version")
		fees := rapid.Int64Range(0, 1_000_000).Draw(t, "fees")
		self := op(v, fees)

		got := domain.HigherVersionWins(self, self)

		if got != self {
			t.Fatalf("merging an op with itself changed it: %+v -> %+v", self, got)
		}
	})
}

func TestHigherVersionWins_TieKeepsCurrent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		v := rapid.Int64Range(0, 1_000_000).Draw(t, "version")
		curFees := rapid.Int64Range(0, 1_000_000).Draw(t, "currentFees")
		incFees := rapid.Int64Range(0, 1_000_000).Draw(t, "incomingFees")

		got := domain.HigherVersionWins(op(v, curFees), op(v, incFees))

		if got.FeesMinor != curFees {
			t.Fatalf("equal versions must keep current (idempotent): got fees %d, want %d", got.FeesMinor, curFees)
		}
	})
}
