#!/usr/bin/env bash
# Enforces the plans/ rules (see CLAUDE.md):
#   1. every file in plans/ is named NNNN-slug.md (or TEMPLATE.md)
#   2. every file in plans/ is linked from README.md
#   3. in a PR (BASE_REF set), at least one NEW plans/NNNN-*.md is added
#   test/plans.sh                       # rules 1-2
#   BASE_REF=origin/main test/plans.sh  # rules 1-3
set -euo pipefail
fail=0
shopt -s nullglob

for f in plans/*; do
  b=$(basename "$f")
  if [[ "$b" != TEMPLATE.md && ! "$b" =~ ^[0-9]{4}-[a-z0-9-]+\.md$ ]]; then
    echo "FAIL: plans/$b must be named NNNN-slug.md"; fail=1
  fi
  if ! grep -Fq "(plans/$b)" README.md; then
    echo "FAIL: plans/$b is not linked from README.md (expected a link to plans/$b)"; fail=1
  fi
done

if [ -n "${BASE_REF:-}" ]; then
  added=$(git diff --name-only --diff-filter=A "$BASE_REF"...HEAD -- 'plans/[0-9][0-9][0-9][0-9]-*.md')
  if [ -z "$added" ]; then
    echo "FAIL: this PR adds no new plan. Copy plans/TEMPLATE.md to plans/NNNN-your-change.md,"
    echo "      fill it in, and link it from README.md."
    fail=1
  else
    echo "new plan(s): $added"
  fi
fi

[ $fail -eq 0 ] && echo "plans ok"
exit $fail
