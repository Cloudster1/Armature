#!/bin/sh
# Judges branch names and commit subjects by the convention the release script reads
# its bump from, with only sh, git, grep and sed, since the push hook runs it on the host.
set -u

TYPES='feat|fix|docs|chore|refactor|test|perf|ci|build|revert'
TYPE_LIST='feat, fix, docs, chore, refactor, test, perf, ci, build or revert'
BRANCH_RE="^($TYPES)/[0-9]+-[a-z0-9][a-z0-9-]*\$"
RELEASE_RE='^release/v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$'
SUBJECT_RE="^($TYPES)(\([a-z0-9-]+\))?!?: [^[:space:]]"
BARE_RE="^($TYPES)(\([a-z0-9-]+\))?!?:[[:space:]]*\$"
BREAKING_RE='^BREAKING[ -]CHANGE: '
EXAMPLE='fix(search): open the bar for ticked rows'

lower() {
	sed 'y/ABCDEFGHIJKLMNOPQRSTUVWXYZ/abcdefghijklmnopqrstuvwxyz/'
}

matches() {
	printf '%s\n' "$1" | grep -Eq -- "$2"
}

conventional() {
	matches "$1" "$SUBJECT_RE"
}

# git writes these itself; a merge carries the type of what it merges.
merge_subject() {
	matches "$1" '^Merge .'
}

revert_subject() {
	matches "$1" '^Revert ".+"'
}

# The type a refused name most likely meant, for the example its refusal gives.
likely_type() {
	word=$(printf '%s\n' "$1" | lower)
	case $word in
	feat | fix | docs | chore | refactor | test | perf | ci | build | revert) printf '%s\n' "$word" ;;
	bug | bugfix | hotfix) echo fix ;;
	doc) echo docs ;;
	tests) echo test ;;
	*) echo feat ;;
	esac
}

# The refused subject rewritten as the convention would have it.
example_subject() {
	s=$1
	word=$(printf '%s\n' "$s" | sed -nE 's/^([A-Za-z]+)(\([^)]*\))?!?:.*/\1/p')
	scope= bang= rest=$s
	if [ -n "$word" ]; then
		scope=$(printf '%s\n' "$s" | sed -nE 's/^[A-Za-z]+\(([^)]*)\)!?:.*/\1/p' |
			lower | sed -E 's/[^a-z0-9]+/-/g; s/^-+//; s/-+$//')
		bang=$(printf '%s\n' "$s" | sed -nE 's/^[A-Za-z]+(\([^)]*\))?(!?):.*/\2/p')
		rest=$(printf '%s\n' "$s" | sed -E 's/^[A-Za-z]+(\([^)]*\))?!?:[[:space:]]*//')
	fi
	tail=${rest#?}
	first=${rest%"$tail"}
	case $tail in
	[A-Z]*) ;;
	*) first=$(printf '%s\n' "$first" | lower) ;;
	esac
	printf '%s%s%s: %s%s\n' "$(likely_type "$word")" "${scope:+($scope)}" "$bang" "$first" "$tail"
}

# Prints why SUBJECT is refused, or nothing when it passes.
subject_refusal() {
	s=$1
	if [ -z "$s" ]; then
		echo "Write the subject: a type, a colon and what the change does, such as \"$EXAMPLE\"."
	elif conventional "$s" || merge_subject "$s" || revert_subject "$s"; then
		return 0
	elif matches "$s" '^(fixup|squash|amend)! '; then
		echo "\"$s\" is a fixup git has not squashed yet. Squash it into the commit it fixes before pushing, with git rebase -i --autosquash."
	elif matches "$s" "$BARE_RE"; then
		echo "\"$s\" names its type but not the change. After the colon and a space, say what it changes, such as \"$EXAMPLE\"."
	else
		echo "\"$s\" is not a commit subject the release script can read. Start it with its type, such as \"$(example_subject "$s")\", using one of $TYPE_LIST."
	fi
}

branch_refusal() {
	name=$1
	if [ -z "$name" ]; then
		echo 'Name the branch <type>/<issue>-<slug>, such as fix/12-open-the-bar.'
		return 0
	fi
	if matches "$name" "$BRANCH_RE" || matches "$name" "$RELEASE_RE" || [ "$name" = main ]; then
		return 0
	fi
	case $name in
	release/*)
		version=${name#release/}
		version=${version#v}
		example=release/v0.2.0
		matches "$version" '^[0-9]+\.[0-9]+\.[0-9]+' && example="release/v$version"
		echo "\"$name\" is not a release branch name the release script can read. Name it release/vX.Y.Z, such as $example."
		return 0
		;;
	*/*) word=${name%%/*} rest=${name#*/} ;;
	*) word= rest=$name ;;
	esac
	slug=$(printf '%s\n' "$rest" | lower | sed -E 's/[^a-z0-9]+/-/g; s/^-+//; s/-+$//')
	issue=$(printf '%s\n' "$slug" | sed -nE 's/^([0-9]+)-[a-z0-9].*/\1/p')
	if [ -z "$issue" ]; then
		issue='<issue>'
		slug="$issue-${slug:-<slug>}"
	fi
	example="$(likely_type "$word")/$slug"
	echo "\"$name\" is not a branch name the release script can read. Name it <type>/<issue>-<slug>, such as $example, and let gh make it from the issue: gh issue develop $issue --name $example --base main --checkout."
}

# Prints the type SUBJECT tells the release script: a merge of a pull request
# carries its branch's type, any other merge says "merge".
subject_type() {
	s=$1
	if conventional "$s"; then
		printf '%s\n' "$s" | sed -E 's/^([a-z]+)(\([a-z0-9-]+\))?(!?):.*/\1\3/'
	elif merge_subject "$s"; then
		merged=$(printf '%s\n' "$s" | sed -nE "s,^Merge pull request #[0-9]+ from [^/ ]+/($TYPES)/.*,\\1,p")
		printf '%s\n' "${merged:-merge}"
	else
		echo revert
	fi
}

# Prints REV's type, or why it is refused on stderr with status 1.
judge_commit() {
	rev=$1
	subject=$(git log -1 --format=%s "$rev")
	refusal=$(subject_refusal "$subject")
	if [ -z "$refusal" ] && merge_subject "$subject" && ! conventional "$subject"; then
		case $(git log -1 --format=%p "$rev") in
		*' '*) ;;
		*) refusal="\"$subject\" has one parent, and \"Merge\" begins only a merge commit's subject. Start it with its type, such as \"$(example_subject "$subject")\"." ;;
		esac
	fi
	if [ -n "$refusal" ]; then
		printf '%s\n' "$refusal" >&2
		return 1
	fi
	type=$(subject_type "$subject")
	case $type in
	merge | *!) ;;
	*) git log -1 --format=%b "$rev" | grep -Eq "$BREAKING_RE" && type="$type!" ;;
	esac
	printf '%s\n' "$type"
}

revision() {
	git rev-parse -q --verify "$1^{commit}" >/dev/null 2>&1 && return 0
	echo "\"$1\" is not a revision git knows here. Fetch it first, or name another, such as origin/main." >&2
	return 1
}

judge_range() {
	base=$1 head=$2
	revision "$base" || return 1
	revision "$head" || return 1
	refused=0
	for rev in $(git rev-list --reverse "$base..$head"); do
		if ! why=$(judge_commit "$rev" 2>&1 >/dev/null); then
			printf '%s %s\n' "$(git rev-parse --short "$rev")" "$why" >&2
			refused=$((refused + 1))
		fi
	done
	[ "$refused" -eq 0 ] && return 0
	echo "Reword the commits above before pushing, for example with git rebase -i $base and reword in front of each." >&2
	return 1
}

usage() {
	echo 'Name what to judge: branch NAME, subject TEXT, type SUBJECT (prints its type, with ! for a break), commit REV (the same, reading its body too) or range BASE HEAD.' >&2
	exit 2
}

refuse_unless_empty() {
	[ -z "$1" ] && return 0
	printf '%s\n' "$1" >&2
	exit 1
}

[ $# -ge 1 ] || usage
command=$1
shift
case $command in
branch)
	[ $# -eq 1 ] || usage
	refuse_unless_empty "$(branch_refusal "$1")"
	;;
subject)
	[ $# -eq 1 ] || usage
	refuse_unless_empty "$(subject_refusal "$1")"
	;;
type)
	[ $# -eq 1 ] || usage
	refuse_unless_empty "$(subject_refusal "$1")"
	subject_type "$1"
	;;
commit)
	[ $# -eq 1 ] || usage
	revision "$1" || exit 1
	judge_commit "$1" || exit 1
	;;
range)
	[ $# -eq 2 ] || usage
	judge_range "$1" "$2" || exit 1
	;;
*) usage ;;
esac
