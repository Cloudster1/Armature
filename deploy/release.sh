#!/bin/sh
# Cuts a release in two steps, because main takes no direct commits: release.sh [X.Y.Z]
# opens a pull request for release/vX.Y.Z, and release.sh tag tags its merge.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
conventions="$here/conventions.sh"
cd "$(git rev-parse --show-toplevel)"

SEMVER='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
RELEASE_SUBJECT='^chore\(release\): v([^ ]+)( \(#[0-9]+\))?$'
CHART=deploy/charts/armature
GOLDEN=$CHART/tests/golden
nl='
'

fail() {
	printf '%s\n' "$*" >&2
	exit 1
}

commits() {
	if [ "$1" -eq 1 ]; then echo '1 commit'; else echo "$1 commits"; fi
}

# newer A B succeeds when A comes after B, pre-releases ordered as semver
# orders them: rc.2 before rc.10, and any pre-release before its release.
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
	awk -v a="${a_pre#-}" -v b="${b_pre#-}" 'BEGIN {
		na = split(a, x, "."); nb = split(b, y, ".")
		for (i = 1; i <= na && i <= nb; i++) {
			if (x[i] == y[i]) continue
			xn = x[i] ~ /^[0-9]+$/; yn = y[i] ~ /^[0-9]+$/
			if (xn && yn) exit !(x[i] + 0 > y[i] + 0)
			if (xn != yn) exit !yn
			exit !(x[i] > y[i])
		}
		exit !(na > nb)
	}'
}

# Replaces the first n "version" members of a JSON file: the package's own,
# and in the lockfile the root package's second copy of it.
set_json_version() {
	awk -v v="$1" -v n="$3" '
		n > 0 && /"version": "[^"]*"/ { sub(/"version": "[^"]*"/, "\"version\": \"" v "\""); n-- }
		{ print }
	' "$2" >"$2.tmp" && mv "$2.tmp" "$2"
}

# The body of the CHANGELOG section whose heading starts HEADING, without the
# blank lines around it.
changelog_section() {
	awk -v head="$1" '
		f && /^## / { exit }
		f { line[++n] = $0; if (NF) { last = n; if (!first) first = n } }
		$0 == head || index($0, head " ") == 1 { f = 1 }
		END { for (i = first; first && i <= last; i++) print line[i] }
	' CHANGELOG.md
}

# A release must be what the remote has: on main, clean, and neither behind
# nor ahead of what main follows, or the tag would name code nobody can fetch.
check_checkout() {
	branch=$(git rev-parse --abbrev-ref HEAD)
	[ "$branch" = main ] ||
		fail "Releases are cut from main, and this is $branch; switch to it with git switch main and run it again."
	[ -z "$(git status --porcelain)" ] ||
		fail "The working tree has changes; commit or stash them, then run it again."
	upstream=$(git rev-parse -q --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null) ||
		fail "main follows no remote branch, so nothing says the release is what the remote has; set it with git branch -u origin/main."
	remote=${upstream%%/*}
	git fetch -q --tags "$remote" ||
		fail "Could not reach $remote to check that main is up to date; connect and run it again."
	behind=$(git rev-list --count "HEAD..$upstream")
	[ "$behind" -eq 0 ] ||
		fail "main is $(commits "$behind") behind $upstream, which the release would leave out; bring it up to date with git pull --ff-only and run it again."
	ahead=$(git rev-list --count "$upstream..HEAD")
	[ "$ahead" -eq 0 ] ||
		fail "main has $(commits "$ahead") that $upstream does not, and main takes no direct commits; move them to a branch of their own, reset main with git reset --hard $upstream, and run it again."
}

# Sets new, rule and decided from the commits since the tag LAST: a break
# raises MAJOR (MINOR below 1.0.0), a feat MINOR, anything else PATCH.
infer() {
	last=$1
	[ -n "$last" ] ||
		fail "No release has been tagged yet, so there is nothing to count the next version from; name the first one by hand, such as make release VERSION=$(cat VERSION 2>/dev/null || echo 0.1.0)."
	base=${last#v}
	echo "$base" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' ||
		fail "The last release, $last, is a pre-release, which says nothing about the next number; name it by hand, such as make release VERSION=${base%%-*}."
	revs=$(git rev-list --reverse "$last..HEAD")
	[ -n "$revs" ] || fail "Nothing has been committed since $last, so there is nothing to release."
	breaking= feats= all= count=0
	for rev in $revs; do
		# A subject the convention refuses predates it, and counts as a patch.
		type=$(sh "$conventions" commit "$rev" 2>/dev/null) || type=other
		line="  $(git log -1 --format='%h %s' "$rev")$nl"
		case $type in
		*!) breaking=$breaking$line ;;
		feat) feats=$feats$line ;;
		esac
		all=$all$line
		count=$((count + 1))
	done
	major=${base%%.*} rest=${base#*.}
	minor=${rest%%.*} patch=${rest#*.}
	if [ -n "$breaking" ] && [ "$major" -eq 0 ]; then
		new=$major.$((minor + 1)).0 decided=$breaking
		rule="A breaking change since $last makes a minor release while the version is below 1.0.0: $base to $new."
	elif [ -n "$breaking" ]; then
		new=$((major + 1)).0.0 decided=$breaking
		rule="A breaking change since $last makes a major release: $base to $new."
	elif [ -n "$feats" ]; then
		new=$major.$((minor + 1)).0 decided=$feats
		rule="A feat since $last makes a minor release: $base to $new."
	else
		new=$major.$minor.$((patch + 1)) decided=$all
		rule="Nothing since $last is a feat or a break, so this is a patch release: $base to $new."
	fi
	how="inferred from the $(commits "$count") since $last"
}

prepare() {
	check_checkout
	last=$(git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || true)
	rule= decided=
	if [ -n "$new" ]; then
		how='given by hand'
	else
		infer "$last"
	fi
	echo "$new" | grep -Eq "$SEMVER" ||
		fail "$new is not a semantic version; write it as MAJOR.MINOR.PATCH, for example 0.2.0."
	git rev-parse -q --verify "refs/tags/v$new" >/dev/null &&
		fail "v$new is already tagged; choose the next version."
	for ref in "refs/heads/release/v$new" "refs/remotes/$remote/release/v$new"; do
		git rev-parse -q --verify "$ref" >/dev/null &&
			fail "A branch release/v$new already exists; merge or close its pull request and delete the branch, or choose the next version."
	done
	if [ -n "$last" ]; then
		newer "$new" "${last#v}" ||
			fail "$new is not newer than the last release, ${last#v}; choose a higher version."
	fi
	notes=$(changelog_section '## Unreleased')
	[ -n "$notes" ] || fail "CHANGELOG.md says nothing under ## Unreleased; write what changed there first."

	echo "Armature $new, $how."
	[ -z "$rule" ] || printf '%s\nDecided by:\n%s' "$rule" "$decided"
	printf 'Release notes, from ## Unreleased in CHANGELOG.md:\n%s\n' "$notes" | sed '2,$s/^/  /'
	if [ "$dry_run" = 1 ]; then
		echo 'Dry run: nothing was written, committed or pushed.'
		exit 0
	fi

	command -v gh >/dev/null 2>&1 ||
		fail "The release opens its pull request with the GitHub CLI, which is not installed; install gh, sign in with gh auth login and run it again."
	git var GIT_COMMITTER_IDENT >/dev/null 2>&1 ||
		fail "Git does not know who is releasing; set user.name and user.email, or pass GIT_AUTHOR_NAME, GIT_AUTHOR_EMAIL, GIT_COMMITTER_NAME and GIT_COMMITTER_EMAIL."

	git switch -q -c "release/v$new"
	trap 'echo "The release stopped on release/v$new. Fix it there, or drop it with git switch -f main && git branch -D release/v$new, and run it again." >&2' EXIT

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
	git commit -q -m "chore(release): v$new"
	git push -q -u "$remote" "release/v$new"
	trap - EXIT

	body=$(mktemp)
	printf '%s\n\nOnce this is merged, tag the merge from an up-to-date main with `make release-tag`; the tag builds and publishes the release.\n' \
		"$notes" >"$body"
	gh pr create --base main --head "release/v$new" --title "chore(release): v$new" --body-file "$body" ||
		fail "release/v$new is pushed, but gh could not open its pull request; open it with gh pr create --base main --head release/v$new --body-file $body."
	rm -f "$body"
	git switch -q main
	echo "Once the pull request is merged: git pull --ff-only && make release-tag."
}

# The tag is pushed from here, with the releaser's own credentials: a tag a
# workflow pushes with its GITHUB_TOKEN starts no other workflow, release.yml included.
tag_merge() {
	[ -z "$new" ] ||
		fail "make release-tag reads the version from VERSION, which its release branch wrote; run it without one."
	check_checkout
	version=$(cat VERSION)
	tag=v$version
	# HEAD is the release commit itself after a squash or rebase, its merge otherwise.
	released=$(git log -1 --format=%s HEAD | sed -nE "s/$RELEASE_SUBJECT/\\1/p")
	if [ -z "$released" ] && git rev-parse -q --verify 'HEAD^2' >/dev/null; then
		released=$(git log -1 --format=%s 'HEAD^2' | sed -nE "s/$RELEASE_SUBJECT/\\1/p")
	fi
	[ -n "$released" ] ||
		fail "HEAD is not the merge of a release branch, and only that merge is tagged; merge the pull request make release opened, bring main up to date with git pull --ff-only and run it again."
	[ "$released" = "$version" ] ||
		fail "HEAD merges release/v$released, but VERSION says $version; that branch did not write the release, so cut it again with make release."

	if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
		[ "$(git rev-parse "$tag^{commit}")" = "$(git rev-parse HEAD)" ] ||
			fail "$tag already names another commit; a published version is never moved, so release the next one with make release."
		if git ls-remote --exit-code --tags "$remote" "refs/tags/$tag" >/dev/null 2>&1; then
			echo "$tag is already tagged here and on $remote; there is nothing left to do."
			exit 0
		fi
		[ "$dry_run" = 0 ] || { echo "Dry run: $tag is tagged here and would be pushed to $remote."; exit 0; }
	else
		notes=$(changelog_section "## $version")
		[ -n "$notes" ] ||
			fail "CHANGELOG.md has no section ## $version to tag the release with; the release branch writes it, so cut the release again with make release."
		printf 'Tag %s as %s, with the notes:\n%s\n' "$(git rev-parse --short HEAD)" "$tag" "$notes" | sed '2,$s/^/  /'
		if [ "$dry_run" = 1 ]; then
			echo 'Dry run: nothing was tagged or pushed.'
			exit 0
		fi
		message=$(mktemp)
		printf 'Armature %s\n\n%s\n' "$version" "$notes" >"$message"
		git tag -a "$tag" --cleanup=whitespace -F "$message"
		rm -f "$message"
	fi
	git push -q "$remote" "refs/tags/$tag" ||
		fail "$tag is tagged here, but could not be pushed to $remote; run make release-tag again to push it."
	echo "Pushed $tag; the Release workflow builds and publishes it from there."
}

mode=prepare new= dry_run=0
for arg in "$@"; do
	case $arg in
	'') ;;
	--dry-run) dry_run=1 ;;
	tag) mode=tag ;;
	-*) fail "$arg is not an option of the release; the only one is --dry-run." ;;
	*)
		[ -z "$new" ] || fail "Name one version to release, not $new and $arg."
		new=$arg
		;;
	esac
done

if [ "$mode" = tag ]; then
	tag_merge
else
	prepare
fi
