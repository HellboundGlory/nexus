#!/usr/bin/env bash
# Generates a grouped markdown changelog from a git commit range.
#
# Usage: changelog.sh <range|ref>     e.g. changelog.sh prevtag..HEAD
#
# Groups conventional-commit subjects by type (feat/fix/perf/refactor/docs/ci/
# chore/test/build/style/revert/other), emitting "### <Heading>" sections in a
# stable order. Merge commits are omitted. Purely cosmetic; used by the
# docker-publish workflow to build a GitHub Release body for each image build.
set -euo pipefail

range="${1:-HEAD}"

declare -A label=(
  [feat]="Features"
  [fix]="Fixes"
  [perf]="Performance"
  [refactor]="Refactors"
  [docs]="Docs"
  [ci]="CI"
  [chore]="Chores"
  [test]="Tests"
  [build]="Build"
  [style]="Style"
  [revert]="Reverts"
)
order=(feat fix perf refactor docs ci chore test build style revert other)

declare -A buckets
# mapfile of "%h %s" lines, no merge commits.
mapfile -t lines <<< "$(git log "$range" --no-merges --format='%h %s')"

for line in "${lines[@]:-}"; do
  # Guard: git log printed nothing or an error line.
  if [[ -z "$line" || "$line" != *" "* ]]; then
    continue
  fi
  hash=${line%% *}
  body=${line#* }
  type="other"
  if [[ "$body" =~ ^(feat|fix|perf|refactor|docs|ci|chore|test|build|style|revert)(\(.*\))?: ]]; then
    type="${BASH_REMATCH[1]}"
  fi
  buckets["$type"]+="- \`${hash:0:7}\` ${body}"$'\n'
done

emitted=0
for t in "${order[@]}"; do
  if [[ -n "${buckets[$t]:-}" ]]; then
    printf '### %s\n\n%s\n' "${label[$t]:-${t^}}" "${buckets[$t]}"
    emitted=1
  fi
done

if [[ "$emitted" -eq 0 ]]; then
  printf 'No user-facing commits in this range.\n'
fi
