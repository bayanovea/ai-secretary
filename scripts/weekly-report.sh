#!/bin/sh
# Example of a recurring duty: adds "Write the weekly report" every time it
# runs, due on the coming Friday. Meant to be called by cron or launchd on a
# Monday morning; see docs/RECURRING.md.
#
# Idempotent on purpose: if last week's report is still open the duty is not
# added again. A pile of identical duties is how a tracker stops being read, and
# the CLI has no notion of recurrence — this script is the whole of it.
set -eu

BIN="${SECRETARY_BIN:-secretary}"
LABEL="weekly-report"

if "$BIN" list --label "$LABEL" --color never | grep -q 'T-[0-9]'; then
	echo "$(date +%F): last week's report is still open, nothing added"
	exit 0
fi

"$BIN" add \
	--title "Write the weekly report" \
	--tier 2 \
	--from "self" \
	--due "$(date -v+Fri +%F 2>/dev/null || date -d 'next friday' +%F)" \
	--effort 2h \
	--section "Work" \
	--context @computer \
	--labels "$LABEL"
