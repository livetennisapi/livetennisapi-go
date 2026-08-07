#!/bin/sh
# truthcheck.sh — pins this repo's product copy to the Live Tennis API's
# ground truth. POSIX sh, no dependencies beyond git and grep. Run from
# anywhere; CI runs it on every push.
#
# Forbidden: stale quota numbers (the 2026-08-06 grid is FREE 100/day,
# BASIC 1,000/day, PRO 10,000/day, ULTRA 500,000/day), the wrong docs URL
# (docs.livetennisapi.com is canonical, livetennisapi.com/docs is not),
# personal handles in machine-read metadata, and the "midnight UTC" reset
# myth (the daily reset is an absolute local-midnight-derived instant).
set -eu

cd "$(git rev-parse --show-toplevel)"

# CHANGELOG entries may describe history; this script may name the patterns.
FILES=$(git ls-files | grep -v -e '^CHANGELOG\.md$' -e '^scripts/truthcheck\.sh$')

fail=0

forbid() {
    pattern=$1
    why=$2
    # shellcheck disable=SC2086
    hits=$(printf '%s\n' "$FILES" | xargs grep -niE -- "$pattern" 2>/dev/null || true)
    if [ -n "$hits" ]; then
        printf 'FORBIDDEN (%s):\n%s\n' "$why" "$hits"
        fail=1
    fi
}

# Stale FREE-tier quota: 100k/day predates 2026-08-06.
forbid '(100[,.]?000|100k)[^0-9]{0,40}(/ ?day|per ?.?day|daily|a day)' 'stale 100k/day quota'
forbid '(/ ?day|per ?.?day|daily)[^0-9]{0,40}(100[,.]?000|100k)' 'stale 100k/day quota'

# Stale FREE-tier quota: free was never 1,000/day under the current grid.
forbid 'free[^0-9]{0,60}(1[,.]?000|1k)[^0-9]{0,20}(/ ?day|per ?.?day|daily|a day)' 'free tier is 100/day, not 1k'

# Wrong docs URL (docs.livetennisapi.com is the canonical docs host).
forbid '(^|[^.a-z])livetennisapi\.com/docs' 'use docs.livetennisapi.com'

# No personal handles in repo copy or machine-read metadata.
forbid 'bensynapse' 'use the org identity'

# The daily reset is a local-midnight-derived absolute instant.
forbid 'midnight utc' 'daily reset is not a fixed UTC hour'

# If the repo states quotas at all, it must state the current FREE number and
# point at the canonical docs host.
# shellcheck disable=SC2086
if printf '%s\n' "$FILES" | xargs grep -qiE 'requests?/(min|day)|per.day|quota' 2>/dev/null; then
    # shellcheck disable=SC2086
    if ! printf '%s\n' "$FILES" | xargs grep -qE '100(/day| requests/day)' 2>/dev/null; then
        echo 'MISSING: the FREE quota must be stated as 100/day'
        fail=1
    fi
    # shellcheck disable=SC2086
    if ! printf '%s\n' "$FILES" | xargs grep -q 'docs\.livetennisapi\.com' 2>/dev/null; then
        echo 'MISSING: docs.livetennisapi.com'
        fail=1
    fi
fi

if [ "$fail" -ne 0 ]; then
    echo 'truthcheck: FAILED'
    exit 1
fi
echo 'truthcheck: ok'
