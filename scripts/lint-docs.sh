#!/usr/bin/env bash
# The rule of the SDK that services do not share, so it stays out of
# lint-rules.sh, whose copy the full template ships: every package has a
# doc.go with the overview, the usage and the options of the package. Checks
# tracked files and new files that are not ignored yet; a package of generated
# code only is skipped.
#
# Usage: lint-docs.sh
set -euo pipefail

# grep -L drops a file deleted but still in the index: it cannot be read.
nodoc=$(git ls-files --cached --others --exclude-standard '*.go' |
	grep -v '_test\.go$' |
	xargs grep -L '^// Code generated .* DO NOT EDIT\.$' 2>/dev/null |
	xargs -n1 dirname | sort -u |
	while read -r d; do [ ! -f "$d/doc.go" ] && echo "$d"; done || true)
if [ -n "$nodoc" ]; then
	echo "$nodoc"
	echo 'lint-docs: package without doc.go — move the package comment there'
	exit 1
fi
