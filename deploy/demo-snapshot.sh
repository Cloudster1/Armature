#!/bin/sh
# Takes the suite demo's snapshot (#300, #486): every issue, milestone and label of Armature, Exciter, Stator
# and Rotor, without pull requests, as backend/internal/demo/suite/<tool>.json. Run on the host by make demo-snapshot, with gh signed in.
set -eu

OUT=${1:-$(cd "$(dirname "$0")/.." && pwd)/backend/internal/demo/suite}
TAKEN=$(date -u +%Y-%m-%d)
LIMIT=5000

for tool in gh jq; do
	if ! command -v "$tool" >/dev/null; then
		echo "$tool is not installed. Install it and run make demo-snapshot again." >&2
		exit 1
	fi
done

mkdir -p "$OUT"
# The key prefixes the issue keys, which is how Exciter's suite demo finds them: EXC-261 is Cloudster1/Exciter#261.
while read -r file repo key name; do
	commit=$(gh api "repos/$repo/commits/HEAD" --jq .sha)
	# Milestones and labels are taken as lists of their own, so one no issue carries comes over too.
	gh api --paginate "repos/$repo/milestones?state=all&per_page=100" |
		jq -s '[add[] | {number, title, description: (.description // ""), dueOn: .due_on, closed: (.state == "closed")}]
			| sort_by(.number) | map(del(.number))' >"$OUT/$file.milestones.tmp"
	gh api --paginate "repos/$repo/labels?per_page=100" |
		jq -s '[add[] | {name, color, description: (.description // "")}] | sort_by(.name)' >"$OUT/$file.labels.tmp"
	# No login or address is taken: the demo's own people report and work the issues.
	gh issue list -R "$repo" --state all --limit "$LIMIT" \
		--json number,title,state,createdAt,closedAt,labels,milestone,url,body |
		jq --arg repo "$repo" --arg key "$key" --arg name "$name" --arg commit "$commit" --arg taken "$TAKEN" \
			--slurpfile milestones "$OUT/$file.milestones.tmp" --slurpfile labels "$OUT/$file.labels.tmp" '{
			repo: $repo, key: $key, name: $name, commit: $commit, taken: $taken,
			milestones: $milestones[0], labels: $labels[0],
			issues: [.[] | {
				number, title, closed: (.state == "CLOSED"), createdAt, closedAt,
				labels: [.labels[].name] | sort, milestone: (.milestone.title // ""), url,
				# Attribution lines an assistant added say nothing about the work.
				body: (.body | gsub("\r"; "") | gsub("\n*[^\n]*Generated with \\[Claude Code\\][^\n]*"; ""))
			}] | sort_by(.number)
		}' >"$OUT/$file.json.tmp"
	rm "$OUT/$file.milestones.tmp" "$OUT/$file.labels.tmp"
	mv "$OUT/$file.json.tmp" "$OUT/$file.json"
	echo "$repo: $(jq '"\(.issues | length) issues, \(.milestones | length) milestones, \(.labels | length) labels"' -r "$OUT/$file.json") at $(printf %.12s "$commit")"
done <<'EOF'
armature Cloudster1/Armature ARM Armature
exciter Cloudster1/Exciter EXC Exciter
stator praetorianer777/Stator STA Stator
rotor Cloudster1/Rotor ROT Rotor
EOF
