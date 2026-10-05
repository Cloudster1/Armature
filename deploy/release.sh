#!/bin/sh
# Cuts a release: make release VERSION=x.y.z. Main never takes a commit
# directly, so the bump is a release branch merged the same way a feature is.
set -eu
cd "$(dirname "$0")/.."

SEMVER='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
CHART=deploy/charts/armature
GOLDEN=$CHART/tests/golden

fail() {
	printf '%s\n' "$*" >&2
	exit 1
}

# newer A B succeeds when A comes after B. Pre-releases are ordered by sort -V,
# which agrees with semver for the rc.1, rc.2 shape this project would use.
newer() {
	a_core=${1%%-*} b_core=${2%%-*}
	a_pre=${1#"$a_core"} b_pre=${2#"$b_core"}
	for field in major minor patch; do
		a=${a_core%%.*} b=${b_core%%.*}
		a_core=${a_core#*.} b_core=${b_core#*.}
		[ "$a" -gt "$b" ] && return 0
		[ "$a" -lt "$b" ] && return 1
	done
	[ -z "$a_pre" ] && [ -n "$b_pre" ] && return 0
	[ -z "$a_pre" ] || [ -z "$b_pre" ] && return 1
	[ "$a_pre" != "$b_pre" ] && [ "$(printf '%s\n%s\n' "$a_pre" "$b_pre" | sort -V | tail -n1)" = "$a_pre" ]
}

# Replaces the first n "version" members of a JSON file: the package's own,
# and in the lockfile the root package's second copy of it.
set_json_version() {
	awk -v v="$1" -v n="$3" '
		n > 0 && /"version": "[^"]*"/ { sub(/"version": "[^"]*"/, "\"version\": \"" v "\""); n-- }
		{ print }
	' "$2" >"$2.tmp" && mv "$2.tmp" "$2"
}

new=${1:-}
[ -n "$new" ] || fail "Name the release: make release VERSION=x.y.z."
echo "$new" | grep -Eq "$SEMVER" ||
	fail "$new is not a semantic version; write it as MAJOR.MINOR.PATCH, for example 0.2.0."
[ "$(git rev-parse --abbrev-ref HEAD)" = main ] ||
	fail "Releases are cut from main; switch to it with git switch main."
[ -z "$(git status --porcelain)" ] ||
	fail "The working tree has changes; commit or stash them before cutting a release."
git rev-parse -q --verify "refs/tags/v$new" >/dev/null &&
	fail "v$new is already tagged; choose the next version."
git rev-parse -q --verify "refs/heads/release/$new" >/dev/null &&
	fail "A branch release/$new already exists; delete it or choose the next version."

last=$(git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || true)
last=${last#v}
if [ -n "$last" ]; then
	newer "$new" "$last" || fail "$new is not newer than the last release, $last; choose a higher version."
fi

git var GIT_COMMITTER_IDENT >/dev/null 2>&1 ||
	fail "Git does not know who is releasing; set user.name and user.email, or pass GIT_AUTHOR_NAME, GIT_AUTHOR_EMAIL, GIT_COMMITTER_NAME and GIT_COMMITTER_EMAIL."

said=$(awk '/^## Unreleased/ { f = 1; next } /^## / { f = 0 } f' CHANGELOG.md | grep -c '[^[:space:]]' || true)
[ "$said" -gt 0 ] || fail "CHANGELOG.md says nothing under ## Unreleased; write what changed there first."

git switch -q -c "release/$new"

printf '%s\n' "$new" >VERSION
set_json_version "$new" web/package.json 1
set_json_version "$new" web/package-lock.json 2
sed -i -E "s/^version: .*/version: $new/; s/^appVersion: .*/appVersion: \"$new\"/" $CHART/Chart.yaml
awk -v head="## $new - $(date -u +%F)" '
	/^## Unreleased/ && !done { print; print ""; print head; done = 1; next }
	{ print }
' CHANGELOG.md >CHANGELOG.md.tmp && mv CHANGELOG.md.tmp CHANGELOG.md

[ -d $CHART/charts ] || make --no-print-directory helm-deps
make --no-print-directory helm-template
# The consistency test reads the files just written; a release that disagrees
# with itself stops here, on its own branch.
make --no-print-directory go ARGS="test -count=1 ./internal/buildinfo/"

git add VERSION CHANGELOG.md web/package.json web/package-lock.json $CHART/Chart.yaml $GOLDEN
git commit -q -m "Release $new"
git switch -q main
git merge -q --no-ff "release/$new" -m "Merge release $new"
git tag -a "v$new" -m "Armature $new"

printf 'Tagged v%s on main. Publish it with: git push origin main v%s\n' "$new" "$new"
