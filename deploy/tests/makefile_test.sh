#!/bin/sh
# Holds every Makefile target that mounts a cache directory to creating it first,
# so Docker never makes it for the mount, owned by root (#323).
set -u

here=$(cd "$(dirname "$0")" && pwd)
makefile="$here/../../Makefile"

# Prints "target needed-prerequisite" for each rule whose recipe mounts a cache
# without naming that cache's directories as a prerequisite.
missing=$(awk '
	function check(t, recipe, prereqs) {
		if (t == "") return
		if (recipe ~ /DOCKER_GO|GO_CACHE_VOL|GO_MOD_VOL/ && prereqs !~ /\|.*\$\(GO_CACHES\)/)
			print t " $(GO_CACHES)"
		if (recipe ~ /DOCKER_NODE|NPM_CACHE_VOL/ && prereqs !~ /\|.*\$\(NPM_CACHE_VOL\)/)
			print t " $(NPM_CACHE_VOL)"
	}
	/^[a-zA-Z0-9_-]+:/ && !/:=/ {
		check(target, recipe, prereqs)
		target = $0; sub(/:.*/, "", target)
		prereqs = $0; sub(/^[^:]*:/, "", prereqs); sub(/##.*/, "", prereqs)
		recipe = ""
		next
	}
	/^\t/ { recipe = recipe $0; next }
	END { check(target, recipe, prereqs) }
' "$makefile")

if [ -n "$missing" ]; then
	printf '%s\n' "$missing" | while read -r target dirs; do
		printf 'FAIL make %s mounts a cache without creating it first; add "| %s" to its prerequisites.\n' "$target" "$dirs"
	done
	exit 1
fi

# The check has to see the targets it guards, or a change to the Makefile's
# layout would pass it by finding nothing.
for target in test openapi web-install; do
	if ! grep -q "^$target:" "$makefile"; then
		printf 'FAIL the Makefile has no target %s, which this test expects to check.\n' "$target"
		exit 1
	fi
done

echo "makefile: every target creates the caches it mounts"
