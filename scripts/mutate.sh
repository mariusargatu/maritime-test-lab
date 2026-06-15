#!/usr/bin/env bash
#
# L0.5 mutation-testing gate — wraps go-mutesting (avito-tech fork) over the pure
# domain packages, the logic worth protecting. It answers "do the tests actually
# fail when the code breaks?" by mutating real code and rerunning `go test`.
#
# go-mutesting's labels are INVERTED from the usual convention:
#   PASS  = the mutant was KILLED   (a test went red — good)
#   FAIL  = the mutant SURVIVED     (tests stayed green — a gap)
# The printed "mutation score" IS the kill rate (killed / total); higher is better.
#
# Why floors below 1.0: many generated mutants are *equivalent* (e.g. `== 0` -> `== 1`
# short-circuits, `Pow(2,..)` -> `Pow(3,..)` under a cap, error-string rewrites). They
# cannot be killed by any test, so 100% is unreachable. The floors below are a RATCHET
# set from measured baselines: a drop means a test was weakened or deleted. Raise a
# floor when you add tests; never lower one without saying why.
#
# Usage:  scripts/mutate.sh            run the gate
#         scripts/mutate.sh --self-test  prove the gate goes red on a gappy suite
set -euo pipefail

BIN="${GO_MUTESTING:-./bin/go-mutesting}"

# package <TAB> kill-rate floor. Ratchet — see header.
TARGETS=$(printf '%s\n' \
  "internal/money	0.62" \
  "services/estimator/domain	0.90" \
  "services/voyage/domain	0.70" \
  "clients/onboard-sync/domain	0.46")

# go-mutesting reruns `go test` per mutant, so rapid records a failfile under each
# package's testdata/rapid every run, and the tool drops a report.json in CWD. Both
# are byproducts, not results — wipe them so the gate never dirties the tree.
cleanup() {
  rm -f report.json
  while IFS=$'\t' read -r pkg _; do
    [ -n "$pkg" ] && rm -rf "$pkg/testdata/rapid"
  done <<EOF
$TARGETS
EOF
}
trap cleanup EXIT

# score_of PKG -> prints the kill rate go-mutesting computed for that package.
score_of() {
  "$BIN" "./$1/" 2>/dev/null | awk '/mutation score/{print $5}'
}

# meets FLOOR SCORE -> exit 0 iff SCORE >= FLOOR (float-safe).
meets() {
  awk -v f="$1" -v s="$2" 'BEGIN{exit !(s+0 >= f+0)}'
}

# self_test proves the gate bites: a no-assertion test must let mutants survive, so
# go-mutesting must report a low score AND `meets` must reject it. If either holds
# the wrong way, the tool or our parsing is broken and the gate is worthless.
self_test() {
  local dir score
  dir=$(mktemp -d)
  trap 'rm -rf "$dir"' RETURN
  cat >"$dir/go.mod" <<'EOF'
module mutateselftest
go 1.26
EOF
  cat >"$dir/calc.go" <<'EOF'
package calc
func Apply(a, b int) int { return a + b }
EOF
  cat >"$dir/calc_test.go" <<'EOF'
package calc
import "testing"
func TestApply(t *testing.T) { _ = Apply(2, 3) } // deliberately asserts nothing
EOF

  score=$( (cd "$dir" && "$OLDPWD_BIN" ./... 2>/dev/null) | awk '/mutation score/{print $5}')
  [ -n "$score" ] || { echo "self-test: go-mutesting produced no score — gate is broken"; return 1; }

  if meets 0.50 "$score"; then
    echo "self-test: FAILED — a no-assertion suite scored $score (>=0.50); the gate cannot see gaps"
    return 1
  fi
  if meets 0.50 0.00 || ! meets 0.50 0.60; then
    echo "self-test: FAILED — float comparison helper is wrong"
    return 1
  fi
  echo "self-test: OK — gappy suite scored $score and the floor check rejects it"
}

[ -x "$BIN" ] || { echo "mutate: $BIN not found — run 'make bootstrap'"; exit 1; }

if [ "${1:-}" = "--self-test" ]; then
  OLDPWD_BIN="$(cd "$(dirname "$BIN")" && pwd)/$(basename "$BIN")"
  self_test
  exit $?
fi

echo "L0.5 mutation gate (score = kill rate; PASS=killed, FAIL=survived):"
printf '  %-32s %8s %8s  %s\n' PACKAGE SCORE FLOOR RESULT
fail=0
while IFS=$'\t' read -r pkg floor; do
  [ -z "$pkg" ] && continue
  score=$(score_of "$pkg")
  if [ -z "$score" ]; then
    printf '  %-32s %8s %8s  %s\n' "$pkg" "----" "$floor" "ERROR (no score)"
    fail=1
    continue
  fi
  if meets "$floor" "$score"; then
    printf '  %-32s %8s %8s  %s\n' "$pkg" "$score" "$floor" "ok"
  else
    printf '  %-32s %8s %8s  %s\n' "$pkg" "$score" "$floor" "BELOW FLOOR"
    fail=1
  fi
done <<EOF
$TARGETS
EOF

if [ "$fail" -ne 0 ]; then
  echo "mutate: a package fell below its kill-rate floor — a test was weakened or new logic is untested."
  exit 1
fi
echo "mutate: green"
