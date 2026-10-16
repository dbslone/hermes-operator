#!/usr/bin/env bash
# Runs the unit tests with the race detector and fails if statement coverage of
# internal/... is below COVERAGE_MIN (default 90).
#   test/coverage.sh            # needs a local Go toolchain
#   make cover                  # same, inside Docker
set -euo pipefail
MIN=${COVERAGE_MIN:-90}
OUT=${COVERAGE_OUT:-coverage.out}

go test -count=1 -race -coverprofile="$OUT" ./internal/...
total=$(go tool cover -func="$OUT" | awk '/^total:/ {gsub("%","",$3); print $3}')
echo "total statement coverage: ${total}% (minimum ${MIN}%)"
awk -v t="$total" -v m="$MIN" 'BEGIN { exit (t+0 >= m+0) ? 0 : 1 }' || {
  echo "FAIL: coverage ${total}% is below ${MIN}%"
  echo "Uncovered functions:"; go tool cover -func="$OUT" | awk '$3+0 < 100' | grep -v '^total:'
  exit 1
}
