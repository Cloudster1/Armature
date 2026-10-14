#!/bin/sh
# Holds deploy/conventions.sh and the pre-push hook to the names the release
# script reads, in scratch repositories so nothing touches the checkout.
set -u

here=$(cd "$(dirname "$0")" && pwd)
script="$here/../conventions.sh"
hooks="$here/../../.githooks"

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
export GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL="$scratch/gitconfig"
export GIT_AUTHOR_NAME=Tester GIT_AUTHOR_EMAIL=tester@armature.test
export GIT_COMMITTER_NAME=Tester GIT_COMMITTER_EMAIL=tester@armature.test

ran=0
failed=0

fail() {
	failed=$((failed + 1))
	printf 'FAIL %s\n' "$*"
}

# accepts ARGS...: the script succeeds and says nothing on stderr.
accepts() {
	ran=$((ran + 1))
	if ! sh "$script" "$@" >"$scratch/out" 2>"$scratch/err"; then
		fail "conventions.sh $* was refused: $(cat "$scratch/err")"
	elif [ -s "$scratch/err" ]; then
		fail "conventions.sh $* passed but complained: $(cat "$scratch/err")"
	fi
}

# refuses HINT ARGS...: the script fails with a sentence that contains HINT.
refuses() {
	hint=$1
	shift
	ran=$((ran + 1))
	if sh "$script" "$@" >"$scratch/out" 2>"$scratch/err"; then
		fail "conventions.sh $* was accepted"
	elif ! grep -qF -- "$hint" "$scratch/err"; then
		fail "conventions.sh $* was refused without saying $hint: $(cat "$scratch/err")"
	elif ! grep -q '\.$' "$scratch/err"; then
		fail "conventions.sh $* was refused without a sentence: $(cat "$scratch/err")"
	fi
}

# prints WANT ARGS...: the script succeeds and prints exactly WANT.
prints() {
	want=$1
	shift
	ran=$((ran + 1))
	got=$(sh "$script" "$@" 2>"$scratch/err")
	status=$?
	if [ "$status" -ne 0 ]; then
		fail "conventions.sh $* was refused: $(cat "$scratch/err")"
	elif [ "$got" != "$want" ]; then
		fail "conventions.sh $* printed '$got', not '$want'"
	fi
}

# -- branch names

accepts branch feat/243-branch-and-commit-convention
accepts branch fix/12-open-the-bar
accepts branch ci/243-branch-and-commit-convention
for type in docs chore refactor test perf build revert; do
	accepts branch "$type/7-a"
done
accepts branch fix/250-bar2
accepts branch main
accepts branch release/v0.2.0
accepts branch release/v1.10.3

refuses feat/243-remote-link branch feature/243-remote-link
refuses fix/243-bulk-bar branch bugfix/243-bulk-bar
refuses '<issue>-open-the-bar' branch fix/open-the-bar
refuses 'gh issue develop' branch fix/open-the-bar
refuses fix/12-open-the-bar branch fix/12-Open-The-Bar
refuses '<type>/<issue>-<slug>' branch fix/12-
refuses '<type>/<issue>-<slug>' branch fix/12-a/b
refuses '<type>/<issue>-<slug>' branch wip
refuses '<type>/<issue>-<slug>' branch main2
refuses release/v0.3.1 branch release/0.3.1
refuses release/vX.Y.Z branch release/v1.2
refuses 'Name the branch' branch ''

# -- commit subjects

accepts subject 'feat: report the build at /meta'
accepts subject 'fix(search): open the bar for ticked rows below them'
accepts subject 'feat(api)!: drop GET /build'
accepts subject 'refactor!: rename the tables'
accepts subject 'chore(release): v0.2.0'
accepts subject 'test(remote-links): a link says whether it is a page'
for type in docs chore test perf ci build revert; do
	accepts subject "$type: a change"
done
accepts subject 'Merge pull request #250 from Cloudster1/fix/bulk-bar-keeps-rows-still'
accepts subject "Merge branch 'main' into feature/webhook-envelope-version"
accepts subject "Merge remote-tracking branch 'origin/main' into fix/bulk-bar-keeps-rows-still"
accepts subject 'Revert "feat: report the build at /meta"'

refuses '"feat: open the bar for ticked search rows below them"' \
	subject 'Open the bar for ticked search rows below them'
refuses 'feat, fix, docs' subject 'Open the bar'
refuses '"feat: open the bar"' subject 'feature: open the bar'
refuses '"fix: open the bar"' subject 'Fix: open the bar'
refuses '"fix: open the bar"' subject 'fix:open the bar'
refuses '"fix(search): open the bar"' subject 'fix(Search): open the bar'
refuses 'say what it changes' subject 'fix: '
refuses 'say what it changes' subject 'fix(search):'
refuses '"feat: WIP"' subject 'WIP'
refuses 'fixup' subject 'fixup! feat: open the bar'
refuses 'Write the subject' subject ''

# -- what a subject says to the release script

prints feat type 'feat: report the build at /meta'
prints fix type 'fix(search): open the bar'
prints 'feat!' type 'feat(api)!: drop GET /build'
prints fix type 'Merge pull request #250 from Cloudster1/fix/bulk-bar-keeps-rows-still'
prints merge type 'Merge pull request #248 from Cloudster1/feature/remote-link-relation'
prints merge type "Merge branch 'main' into fix/12-open-the-bar"
prints revert type 'Revert "feat: report the build at /meta"'
refuses 'Start it with its type' type 'Open the bar'

# -- a range of commits in a scratch repository

repo="$scratch/repo"
git init -q -b main "$repo"
commit() {
	git -C "$repo" commit -q --allow-empty "$@"
}
commit -m 'Write the first page in plain words'
git -C "$repo" switch -q -c feat/1-first
cd "$repo" || exit 1
accepts range main main
commit -m 'feat: add a first page'
commit -m 'fix(page): keep the title on one line'
accepts range main feat/1-first
prints fix commit feat/1-first

commit -m 'feat: drop the old page' -m 'BREAKING CHANGE: the old page is gone.'
accepts range main feat/1-first
prints 'feat!' commit feat/1-first

git -C "$repo" switch -q main
commit -m 'Keep main moving in plain words'
git -C "$repo" switch -q feat/1-first
git -C "$repo" merge -q --no-ff --no-edit main
accepts range main feat/1-first
prints merge commit feat/1-first

commit -m 'Merge the two lists'
refuses 'only a merge' range main feat/1-first
git -C "$repo" reset -q --hard HEAD~1

commit -m 'Open the bar' -m 'BREAKING CHANGE: a trailer does not make the subject readable.'
bad=$(git -C "$repo" rev-parse --short HEAD)
refuses "$bad" range main feat/1-first
refuses '"feat: open the bar"' range main feat/1-first
refuses 'git rebase -i main' range main feat/1-first
git -C "$repo" reset -q --hard HEAD~1
refuses 'not a revision' range main no-such-branch
cd "$scratch" || exit 1

# -- the pre-push hook, against a bare remote

remote="$scratch/remote.git"
clone="$scratch/clone"
git init -q --bare -b main "$remote"
git -C "$repo" push -q "$remote" main 2>/dev/null
git clone -q "$remote" "$clone" 2>/dev/null
git -C "$clone" config core.hooksPath "$hooks"

# pushes HINT ARGS...: git push ARGS from the clone succeeds, or with HINT
# other than "-" fails saying HINT.
pushes() {
	hint=$1
	shift
	ran=$((ran + 1))
	if git -C "$clone" push -q origin "$@" >"$scratch/out" 2>"$scratch/err"; then
		[ "$hint" = - ] || fail "git push $* was accepted"
	elif [ "$hint" = - ]; then
		fail "git push $* was refused: $(cat "$scratch/err")"
	elif ! grep -qF -- "$hint" "$scratch/err"; then
		fail "git push $* was refused without saying $hint: $(cat "$scratch/err")"
	fi
}

git -C "$clone" switch -q -c feature/2-old-name
git -C "$clone" commit -q --allow-empty -m 'feat: add a second page'
pushes 'feat/2-old-name' feature/2-old-name
pushes - feature/2-old-name:fix/2-second-page
git -C "$clone" commit -q --allow-empty -m 'Add a third page'
pushes '"feat: add a third page"' feature/2-old-name:fix/2-second-page
git -C "$clone" reset -q --hard HEAD~1
pushes - :fix/2-second-page
git -C "$clone" tag -a v0.1.0 -m 'A tag is not a branch'
pushes - v0.1.0

if [ "$failed" -gt 0 ]; then
	printf '%d of %d checks failed\n' "$failed" "$ran"
	exit 1
fi
printf 'ok  %d checks of branch names, commit subjects and the push hook\n' "$ran"
