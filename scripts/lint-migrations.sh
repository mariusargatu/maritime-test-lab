#!/usr/bin/env bash
# The DB DDL breaking-change gate (D-049) — the database twin of `buf breaking`
# and the seeded Schema-Registry FULL check. It lints goose migrations added or
# changed versus main for contract-breaking DDL (drop / rename / retype column
# or table, add NOT NULL without default, unsafe unique constraint) using squawk,
# then proves the gate can fail with a red self-test.
#
# Two house rules are baked in:
#   1. Up-blocks only. A goose file's Down block legitimately DROPs things on
#      rollback; linting the whole file would flag every rollback. We gate the
#      forward (apply) DDL — the direction that can break a live consumer.
#   2. Delta vs main, like buf breaking / graphql-inspector diff. Migrations are
#      append-only; only what changed versus main can introduce a new break.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

MIGDIR="services/voyage/adapters/postgres/migrations"
FIXTURE="$MIGDIR/testdata/breaking_drop_column.sql"

# extract_up prints a goose migration's Up block (between the +goose Up and
# +goose Down markers), dropping the markers themselves.
extract_up() { awk '/-- \+goose Up/{f=1; next} /-- \+goose Down/{f=0} f' "$1"; }

# run_squawk lints SQL read from stdin, reporting it under the real file path.
run_squawk() { npx --no-install squawk --config .squawk.toml --stdin-filepath "$1"; }

# --- forward gate: migrations changed vs main must hold the contract ----------
changed="$(git diff --name-only main -- "$MIGDIR" \
  | grep -E '/migrations/[^/]+\.sql$' | grep -v '/testdata/' || true)"

if [ -z "$changed" ]; then
  echo "db-ddl gate: no migrations changed vs main — nothing to lint"
else
  echo "db-ddl gate: linting migrations changed vs main:"
  failed=0
  while IFS= read -r f; do
    [ -z "$f" ] && continue
    echo "  - $f"
    if ! extract_up "$f" | run_squawk "$f"; then
      failed=1
    fi
  done <<EOF
$changed
EOF
  [ "$failed" -eq 0 ] || { echo "db-ddl gate: FAILED — a migration introduces a breaking DDL change"; exit 1; }
fi

# --- red self-test: the breaking fixture MUST be rejected ---------------------
echo "db-ddl gate: red self-test (breaking fixture must be rejected):"
if extract_up "$FIXTURE" | run_squawk "$FIXTURE" >/dev/null 2>&1; then
  echo "db-ddl gate: BROKEN — squawk PASSED a DROP COLUMN fixture; a gate that can't fail is decoration"
  exit 1
fi
echo "db-ddl gate: ok (breaking fixture correctly rejected)"
