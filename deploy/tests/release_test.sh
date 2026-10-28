#!/bin/sh
# Holds deploy/release.sh to the version it infers, what it refuses and what it
# writes, in scratch clones of a bare remote with stand-ins for gh and make.
set -u

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
script="$here/../release.sh"

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL="$scratch/gitconfig"
export GIT_AUTHOR_NAME=Tester GIT_AUTHOR_EMAIL=tester@armature.test
export GIT_COMMITTER_NAME=Tester GIT_COMMITTER_EMAIL=tester@armature.test
# The checkout may be a worktree whose git directory is not mounted, and git
# refuses to run inside it.
cd "$scratch" || exit 1
git config --global init.defaultBranch main

# The stand-ins write down what they were asked, so a test can say what the
# script would have opened on GitHub or rendered in docker.
mkdir "$scratch/bin"
cat >"$scratch/bin/gh" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$STANDIN_LOG/gh"
while [ $# -gt 0 ]; do
	[ "$1" = --body-file ] && cat "$2" >>"$STANDIN_LOG/gh-body"
	shift
done
echo https://github.com/example/armature/pull/7
EOF
cat >"$scratch/bin/make" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"$STANDIN_LOG/make"
EOF
chmod +x "$scratch/bin/gh" "$scratch/bin/make"
export PATH="$scratch/bin:$PATH"

ran=0
failed=0
n=0

fail() {
	failed=$((failed + 1))
	printf 'FAIL %s\n' "$*"
}

# seed_version TAG: the seed's copies of the versioned files say TAG's version.
seed_version() {
	to=${1#v}
	from=$(cat "$dir/seed/VERSION")
	echo "$to" >"$dir/seed/VERSION"
	for f in web/package.json:1 web/package-lock.json:2; do
		awk -v from="\"version\": \"$from\"" -v to="\"version\": \"$to\"" -v left="${f##*:}" \
			'left > 0 && index($0, from) { sub(from, to); left-- } { print }' \
			"$dir/seed/${f%:*}" >"$dir/seed/${f%:*}.new" && mv "$dir/seed/${f%:*}.new" "$dir/seed/${f%:*}"
	done
	sed -i -e "s/^version: .*/version: $to/" -e "s/^appVersion: .*/appVersion: \"$to\"/" \
		"$dir/seed/deploy/charts/armature/Chart.yaml"
}

# fixture [TAG]: a clone of a fresh bare remote holding the files a release
# writes, with TAG on its first commit, and the working directory in it.
fixture() {
	n=$((n + 1))
	dir="$scratch/case$n"
	remote="$dir/remote.git"
	work="$dir/work"
	export STANDIN_LOG="$dir/log"
	mkdir -p "$dir/seed/web" "$dir/seed/deploy/charts/armature/tests/golden" "$STANDIN_LOG"
	cp "$root/VERSION" "$dir/seed/VERSION"
	cp "$root/web/package.json" "$root/web/package-lock.json" "$dir/seed/web/"
	cp "$root/deploy/charts/armature/Chart.yaml" "$dir/seed/deploy/charts/armature/"
	# On a release branch the copies carry the very version a case releases,
	# which would leave the release nothing to write, so they get the tag's.
	seed_version "${1:-v0.0.1}"
	echo 'kind: List' >"$dir/seed/deploy/charts/armature/tests/golden/default.yaml"
	printf '# Changelog\n\n## Unreleased\n\n- Added: a first page (#1).\n\n## 0.0.1 - 2026-01-01\n\n- Added: the start.\n' \
		>"$dir/seed/CHANGELOG.md"
	git -C "$dir/seed" init -q
	git -C "$dir/seed" add -A
	git -C "$dir/seed" commit -q -m 'chore: start'
	[ -z "${1:-}" ] || git -C "$dir/seed" tag -a "$1" -m "Armature ${1#v}"
	git init -q --bare "$remote"
	git -C "$dir/seed" push -q "$remote" main --tags 2>/dev/null
	git clone -q "$remote" "$work" 2>/dev/null
	cd "$work" || exit 1
}

# commit SUBJECT [BODY]: an empty commit on main, pushed, as a merged pull
# request leaves it.
commit() {
	git commit -q --allow-empty -m "$1" ${2:+-m "$2"}
	git push -q origin HEAD 2>/dev/null
}

# merged NUMBER BRANCH SUBJECT: pull request NUMBER from BRANCH, holding one
# commit SUBJECT, merged the way GitHub merges it.
merged() {
	git switch -q -c "$2"
	git commit -q --allow-empty -m "$3"
	git switch -q main
	git merge -q --no-ff "$2" -m "Merge pull request #$1 from Cloudster1/$2"
	git branch -q -D "$2"
	git push -q origin main 2>/dev/null
}

release() {
	sh "$script" "$@" >"$dir/out" 2>"$dir/err"
}

# infers WANT HINT: a dry run names WANT and a rule saying HINT.
infers() {
	ran=$((ran + 1))
	if ! release --dry-run; then
		fail "case $n was refused: $(cat "$dir/err")"
	elif ! grep -qF "Armature $1," "$dir/out"; then
		fail "case $n did not infer $1: $(cat "$dir/out")"
	elif ! grep -qF -- "$2" "$dir/out"; then
		fail "case $n inferred $1 without saying $2: $(cat "$dir/out")"
	fi
}

# says TEXT: the last run printed TEXT.
says() {
	ran=$((ran + 1))
	grep -qF -- "$1" "$dir/out" || fail "case $n did not say $1: $(cat "$dir/out")"
}

# refuses HINT ARGS...: the script fails with a sentence that contains HINT.
refuses() {
	hint=$1
	shift
	ran=$((ran + 1))
	if release "$@"; then
		fail "case $n: release.sh $* was accepted"
	elif ! grep -qF -- "$hint" "$dir/err"; then
		fail "case $n: release.sh $* was refused without saying $hint: $(cat "$dir/err")"
	elif ! grep -q '\.$' "$dir/err"; then
		fail "case $n: release.sh $* was refused without a sentence: $(cat "$dir/err")"
	fi
}

# check WHAT CONDITION...: counts one check, failing it with WHAT unless the
# condition holds.
check() {
	what=$1
	shift
	ran=$((ran + 1))
	"$@" || fail "case $n: $what"
}

same() {
	[ "$1" = "$2" ]
}

# The refs of the clone and of the remote, and the state of the tree: what a
# run that writes nothing leaves as it found.
state() {
	git for-each-ref --format='%(refname) %(objectname)'
	git -C "$remote" for-each-ref --format='%(refname) %(objectname)'
	git status --porcelain
	cat "$STANDIN_LOG"/* 2>/dev/null
}

# json_versions FILE COUNT: the first COUNT "version" members of FILE.
json_versions() {
	sed -nE 's/.*"version": "([^"]*)".*/\1/p' "$1" | head -n "$2" | sort -u
}

# -- the version, inferred from the commits since the last tag

fixture v0.1.0
commit 'fix(page): keep the title on one line'
commit 'feat: add a first page'
infers 0.2.0 'A feat since v0.1.0 makes a minor release'
says 'feat: add a first page'

fixture v0.1.0
commit 'fix(page): keep the title on one line'
commit 'docs: say what a page is'
infers 0.1.1 'patch release'
says 'fix(page): keep the title on one line'

fixture v0.4.2
commit 'feat: add a page'
commit 'feat(api)!: drop GET /build'
infers 0.5.0 'below 1.0.0'
says 'feat(api)!: drop GET /build'

fixture v0.4.2
commit 'fix: rename the tables' 'BREAKING CHANGE: the old names are gone.'
infers 0.5.0 'A breaking change since v0.4.2 makes a minor release'

fixture v1.2.3
commit 'feat: add a page'
commit 'refactor!: rename the tables'
infers 2.0.0 'A breaking change since v1.2.3 makes a major release'
says 'refactor!: rename the tables'

# A merge says nothing of its own beyond its branch's type.
fixture v0.1.0
merged 5 feat/5-first-page 'test: cover the first page'
infers 0.2.0 'A feat since v0.1.0 makes a minor release'
says 'Merge pull request #5 from Cloudster1/feat/5-first-page'

fixture v0.1.0
merged 7 fix/7-title 'test: cover the title'
infers 0.1.1 'patch release'

fixture
commit 'feat: add a first page'
refuses 'name the first one by hand' --dry-run
refuses "make release VERSION=$(cat VERSION)" --dry-run
release --dry-run 0.1.0
check 'a first version given by hand was refused' same $? 0
says 'Armature 0.1.0, given by hand'

fixture v0.1.0
refuses 'nothing to release' --dry-run

# -- what the checkout must be

fixture v0.1.0
commit 'feat: add a first page'
git switch -q -c fix/1-a
refuses 'git switch main' --dry-run
refuses 'git switch main' tag

fixture v0.1.0
commit 'feat: add a first page'
echo change >>VERSION
refuses 'commit or stash' --dry-run
git checkout -q VERSION
echo new >new-file
refuses 'commit or stash' --dry-run

fixture v0.1.0
commit 'feat: add a first page'
git clone -q "$remote" "$dir/other" 2>/dev/null
git -C "$dir/other" commit -q --allow-empty -m 'fix: something somebody merged meanwhile'
git -C "$dir/other" push -q origin main 2>/dev/null
refuses 'behind origin/main' --dry-run
refuses 'git pull --ff-only' --dry-run
refuses 'behind origin/main' tag
git pull -q --ff-only
infers 0.2.0 'minor'

fixture v0.1.0
commit 'feat: add a first page'
git commit -q --allow-empty -m 'fix: something committed on main by hand'
refuses 'main takes no direct commits' --dry-run

fixture v0.1.0
git branch -q --unset-upstream
commit 'feat: add a first page'
refuses 'git branch -u origin/main' --dry-run

# -- the version given by hand

fixture v0.2.0
commit 'feat: add a first page'
refuses 'not a semantic version' --dry-run 0.3
refuses 'not newer than the last release, 0.2.0' --dry-run 0.1.9
refuses 'already tagged' --dry-run 0.2.0
release --dry-run 1.0.0-rc.1
check 'a pre-release given by hand was refused' same $? 0
refuses 'one version' --dry-run 0.3.0 0.4.0

fixture v1.0.0-rc.2
commit 'feat: add a first page'
refuses 'not newer' --dry-run 1.0.0-rc.1
release --dry-run 1.0.0-rc.10
check 'rc.10 was not newer than rc.2' same $? 0
release --dry-run 1.0.0
check 'a release was not newer than its pre-release' same $? 0
refuses 'pre-release' --dry-run

fixture v0.1.0
commit 'feat: add a first page'
git push -q origin main:release/v0.2.0 2>/dev/null
refuses 'release/v0.2.0 already exists' --dry-run

fixture v0.1.0
commit 'feat: add a first page'
printf '# Changelog\n\n## Unreleased\n\n## 0.0.1 - 2026-01-01\n\n- Added: the start.\n' >CHANGELOG.md
git commit -q -am 'docs: empty the changelog'
git push -q origin main 2>/dev/null
refuses 'write what changed there first' --dry-run

# -- a dry run writes nothing

fixture v0.1.0
commit 'feat: add a first page'
before=$(state)
release --dry-run
check 'a dry run was refused' same $? 0
says '- Added: a first page (#1).'
says 'Dry run'
check 'a dry run changed the clone, the remote or called gh or make' same "$(state)" "$before"

# -- the first step: a release branch and its pull request

fixture v0.1.0
commit 'feat: add a first page'
main_before=$(git rev-parse main)
release
check "the release was refused: $(cat "$dir/err")" same $? 0
check 'the release left the clone off main' same "$(git rev-parse --abbrev-ref HEAD)" main
check 'the release moved main' same "$(git rev-parse main)" "$main_before"
check 'the release moved the remote main' same "$(git -C "$remote" rev-parse main)" "$main_before"
check 'the release tagged something before its merge' same "$(git tag -l 'v0.2*')$(git -C "$remote" tag -l 'v0.2*')" ''
check 'the release branch was not pushed' git -C "$remote" show-ref -q --verify refs/heads/release/v0.2.0
check 'the release branch is not one commit on main' same "$(git -C "$remote" rev-parse release/v0.2.0^)" "$main_before"
check 'the release commit is not named by the convention' \
	same "$(git -C "$remote" log -1 --format=%s release/v0.2.0)" 'chore(release): v0.2.0'
check 'the release commit touched files that carry no release' same \
	"$(git diff --name-only main origin/release/v0.2.0 | sort | tr '\n' ' ')" \
	'CHANGELOG.md VERSION deploy/charts/armature/Chart.yaml web/package-lock.json web/package.json '

git switch -q release/v0.2.0
check 'VERSION is not 0.2.0' same "$(cat VERSION)" 0.2.0
check 'web/package.json disagrees' same "$(json_versions web/package.json 1)" 0.2.0
check 'web/package-lock.json disagrees' same "$(json_versions web/package-lock.json 2)" 0.2.0
check 'Chart.yaml version disagrees' grep -qx 'version: 0.2.0' deploy/charts/armature/Chart.yaml
check 'Chart.yaml appVersion disagrees' grep -qx 'appVersion: "0.2.0"' deploy/charts/armature/Chart.yaml
check 'the changelog has no section for 0.2.0' \
	same "$(sed -n '3p;5p;7p' CHANGELOG.md)" "$(printf '## Unreleased\n## 0.2.0 - %s\n- Added: a first page (#1).' "$(date -u +%F)")"
git switch -q main

check 'the chart was not rendered' grep -qx -- '--no-print-directory helm-template' "$STANDIN_LOG/make"
check 'the files were not held together' grep -q 'internal/buildinfo' "$STANDIN_LOG/make"
check 'no pull request was opened' \
	grep -qF -- 'pr create --base main --head release/v0.2.0 --title chore(release): v0.2.0' "$STANDIN_LOG/gh"
check 'the pull request does not carry the notes' grep -qF -- '- Added: a first page (#1).' "$STANDIN_LOG/gh-body"
says 'make release-tag'
refuses 'already exists' --dry-run 0.2.0

# -- the second step: the tag on the merge

refuses 'not the merge of a release branch' tag
git clone -q "$remote" "$dir/github" 2>/dev/null
git -C "$dir/github" merge -q --no-ff origin/release/v0.2.0 -m 'Merge pull request #7 from Cloudster1/release/v0.2.0'
git -C "$dir/github" push -q origin main 2>/dev/null
refuses 'behind origin/main' tag
git pull -q --ff-only
merge=$(git rev-parse HEAD)

before=$(state)
release tag --dry-run
check "a dry run of the tag was refused: $(cat "$dir/err")" same $? 0
says '- Added: a first page (#1).'
check 'a dry run of the tag changed something' same "$(state)" "$before"
refuses 'reads the version from VERSION' tag 0.2.0

release tag
check "the tag was refused: $(cat "$dir/err")" same $? 0
check 'the tag did not reach the remote' same "$(git -C "$remote" rev-parse -q --verify 'v0.2.0^{commit}')" "$merge"
check 'the tag is not annotated' same "$(git -C "$remote" cat-file -t v0.2.0)" tag
check 'the tag does not carry the notes' \
	same "$(git -C "$remote" tag -l --format='%(contents:subject)|%(contents:body)' v0.2.0)" \
	"$(printf 'Armature 0.2.0|- Added: a first page (#1).\n')"
check 'the tag moved main' same "$(git -C "$remote" rev-parse main)" "$merge"

release tag
check 'a second run of the tag was refused' same $? 0
says 'nothing left to do'

# A run whose push failed leaves the tag here; the next run pushes it.
fixture v0.1.0
commit 'feat: add a first page'
release 0.2.0
git clone -q "$remote" "$dir/github" 2>/dev/null
git -C "$dir/github" merge -q --no-ff origin/release/v0.2.0 -m 'Merge pull request #8 from Cloudster1/release/v0.2.0'
git -C "$dir/github" push -q origin main 2>/dev/null
git pull -q --ff-only
git tag -a v0.2.0 -m 'Armature 0.2.0'
release tag
check "a tag left by an earlier run was not pushed: $(cat "$dir/err")" same "$(git -C "$remote" tag -l v0.2.0)" v0.2.0

# A squash merge carries the release commit's subject with its pull request.
fixture v0.1.0
commit 'feat: add a first page'
release 0.2.0
git clone -q "$remote" "$dir/github" 2>/dev/null
git -C "$dir/github" merge -q --squash origin/release/v0.2.0 >/dev/null
git -C "$dir/github" commit -q -m 'chore(release): v0.2.0 (#9)'
git -C "$dir/github" push -q origin main 2>/dev/null
git pull -q --ff-only
release tag
check "a squash merge was not tagged: $(cat "$dir/err")" same "$(git -C "$remote" tag -l v0.2.0)" v0.2.0

# A release branch that did not write VERSION is not tagged.
fixture v0.1.0
git switch -q -c release/v0.3.0
git commit -q --allow-empty -m 'chore(release): v0.3.0'
git switch -q main
git merge -q --no-ff release/v0.3.0 -m 'Merge pull request #10 from Cloudster1/release/v0.3.0'
git push -q origin main 2>/dev/null
refuses 'VERSION says' tag

if [ "$failed" -gt 0 ]; then
	printf '%d of %d checks failed\n' "$failed" "$ran"
	exit 1
fi
printf 'ok  %d checks of the release script\n' "$ran"
