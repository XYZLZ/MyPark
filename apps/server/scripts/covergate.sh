#!/bin/sh
# Domain statement-coverage gate (OC-35: statement coverage stands in for the
# 90% branch rule). Usage: covergate.sh <coverprofile> <min-percent>
#
# Sums statements over the domain files and exits 1 below the minimum,
# printing the per-file percentages. Domain files:
#   internal/features/auth/{email,password,code,refresh,session}.go
#   internal/features/vehicles/{plate,limit}.go
#   internal/infrastructure/hashing/*.go   (test files excluded)
set -eu

if [ "$#" -ne 2 ]; then
	echo "usage: $0 <coverprofile> <min-percent>" >&2
	exit 2
fi
profile=$1
min=$2

if [ ! -r "$profile" ]; then
	echo "covergate: cannot read $profile" >&2
	exit 2
fi

exec awk -v min="$min" '
function domain(f) {
	return f ~ /\/internal\/features\/auth\/(email|password|code|refresh|session)\.go$/ ||
	       f ~ /\/internal\/features\/vehicles\/(plate|limit)\.go$/ ||
	       f ~ /\/internal\/infrastructure\/hashing\/[^\/]+\.go$/
}
NR == 1 && /^mode:/ { next }
{
	# "<file>:<start>,<end> <statements> <count>"
	split($1, loc, ":")
	file = loc[1]
	if (file ~ /_test\.go$/ || !domain(file)) next
	stmts[file] += $2
	total += $2
	if ($3 > 0) {
		hit[file] += $2
		covered += $2
	}
	seen[file] = 1
}
END {
	if (total == 0) {
		print "covergate: no domain statements found in the profile" > "/dev/stderr"
		exit 1
	}
	for (f in seen) {
		n = f
		sub(/.*\/apps\/server\//, "", n)
		printf "%6.1f%%  %s (%d/%d)\n", 100 * hit[f] / stmts[f], n, hit[f], stmts[f] | "sort -k2"
	}
	close("sort -k2")
	pct = 100 * covered / total
	if (pct + 1e-9 < min) {
		printf "domain statement coverage: %.1f%% FAIL (minimum %.1f%%)\n", pct, min
		exit 1
	}
	printf "domain statement coverage: %.1f%% OK (minimum %.1f%%)\n", pct, min
}' "$profile"
