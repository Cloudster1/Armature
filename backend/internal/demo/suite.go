package demo

import (
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/armature/armature/backend/internal/bootstrap"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/markdown"
)

// The suite demo (#300, #486) holds the GitHub issues of the four tools, each under its own number, so
// Exciter's requirements and Stator's pages find EXC-261 as Cloudster1/Exciter#261.
//
//go:embed suite/*.json
var suiteFiles embed.FS

// suiteOrder is the snapshot files in the order the demo builds them, and so the order of its steps.
var suiteOrder = []string{"armature", "exciter", "stator", "rotor"}

// SuiteKeys are the suite demo's projects, which it needs free before it starts.
var SuiteKeys = []string{"ARM", "EXC", "STA", "ROT"}

// SuiteSteps is a step per project and one for the links between their issues.
var SuiteSteps = len(suiteOrder) + 1

// suiteSource is how an imported issue's changelog says where it came from.
const suiteSource = "the suite demo's GitHub snapshot"

// Snapshot is one repository's issues, milestones and labels as make demo-snapshot took them.
type Snapshot struct {
	Repo       string            `json:"repo"`
	Key        string            `json:"key"`
	Name       string            `json:"name"`
	Commit     string            `json:"commit"`
	Taken      string            `json:"taken"`
	Milestones []GitHubMilestone `json:"milestones"`
	Labels     []GitHubLabel     `json:"labels"`
	Issues     []GitHubIssue     `json:"issues"`
}

// GitHubMilestone is a milestone as GitHub had it, in the order it was made there.
type GitHubMilestone struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	DueOn       *time.Time `json:"dueOn"`
	Closed      bool       `json:"closed"`
}

// GitHubLabel is a label as GitHub had it; Color is six hex digits.
type GitHubLabel struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

// GitHubIssue is an issue as GitHub had it, without who wrote or worked it.
type GitHubIssue struct {
	Number    int64      `json:"number"`
	Title     string     `json:"title"`
	Closed    bool       `json:"closed"`
	CreatedAt time.Time  `json:"createdAt"`
	ClosedAt  *time.Time `json:"closedAt"`
	Labels    []string   `json:"labels"`
	Milestone string     `json:"milestone"`
	URL       string     `json:"url"`
	Body      string     `json:"body"`
}

// Snapshots reads the embedded snapshot, Armature's first.
func Snapshots() ([]Snapshot, error) {
	out := make([]Snapshot, 0, len(suiteOrder))
	for i, file := range suiteOrder {
		raw, err := suiteFiles.ReadFile("suite/" + file + ".json")
		if err != nil {
			return nil, fmt.Errorf("read the suite demo's %s snapshot: %w", file, err)
		}
		var s Snapshot
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("read the suite demo's %s snapshot: %w", file, err)
		}
		if s.Key != SuiteKeys[i] {
			return nil, fmt.Errorf("the %s snapshot is for %s, not %s; take it again with make demo-snapshot", file, s.Key, SuiteKeys[i])
		}
		out = append(out, s)
	}
	return out, nil
}

// SuitePlan is what the demo writes: the labels, the projects with their issues, and the blocking links between them.
type SuitePlan struct {
	Labels   []label.Input
	Projects []SuiteProject
	Links    []SuiteLink
}

// SuiteProject is one repository as a project.
type SuiteProject struct {
	Key, Name, Description string
	// Milestones are GitHub's, then any an issue names that GitHub no longer lists.
	Milestones []SuiteMilestone
	Issues     []SuiteIssue
}

// SuiteMilestone is a milestone as the project gets it; a closed one is closed once its issues are filed.
type SuiteMilestone struct {
	Name, Description string
	DueOn             *time.Time
	Closed            bool
}

// SuiteIssue is one GitHub issue as it is written here.
type SuiteIssue struct {
	Number      int64
	Summary     string
	Description json.RawMessage
	TypeName    string
	Done        bool
	CreatedAt   time.Time
	ResolvedAt  *time.Time
	Labels      []string
	Milestone   string
	URL         string
	// Reporter and Assignee index the people the demo is given.
	Reporter, Assignee int
}

// Key is the issue's key here, which is the GitHub number under the project's key.
func (i SuiteIssue) Key(project string) string {
	return issue.FormatKey(project, i.Number)
}

// SuiteLink is a dependency: Blocker blocks Blocked.
type SuiteLink struct {
	Blocker, Blocked string
}

var (
	sectionHeading = regexp.MustCompile(`^##[ \t]+(.+?)[ \t]*$`)
	dependsInline  = regexp.MustCompile(`(?i)^\**depends on\**:?\**[ \t]*(.*)$`)
	issueReference = regexp.MustCompile(`([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)?#([0-9]+)`)
	userStory      = regexp.MustCompile(`^##[ \t]*User story`)
	bugTitle       = regexp.MustCompile(`(?i)^(fix|bug)\b`)
	labelSpace     = regexp.MustCompile(`\s+`)
)

// dependsSection is the heading under which an issue lists what it waits for.
const dependsSection = "depends on"

// PlanSuite maps the snapshots to what the demo writes, handing the work round people by issue number.
func PlanSuite(snapshots []Snapshot, people int) SuitePlan {
	people = max(people, 1)
	keyOf := map[string]string{}
	known := map[string]bool{}
	for _, s := range snapshots {
		keyOf[strings.ToLower(s.Repo)] = s.Key
		for _, gh := range s.Issues {
			known[issue.FormatKey(s.Key, gh.Number)] = true
		}
	}
	var plan SuitePlan
	linked := map[[2]string]bool{}
	labelled := map[string]bool{}
	for _, s := range snapshots {
		// Labels are shared by every project, so the first repository to have a word gives its colour.
		for _, gh := range s.Labels {
			names := labels([]string{gh.Name})
			if len(names) == 0 || labelled[strings.ToLower(names[0])] {
				continue
			}
			labelled[strings.ToLower(names[0])] = true
			plan.Labels = append(plan.Labels, label.Input{Name: names[0], Color: labelColor(gh.Color, names[0])})
		}
	}
	for _, s := range snapshots {
		p := SuiteProject{
			Key: s.Key, Name: s.Name,
			Description: fmt.Sprintf("The issues of %s on GitHub, as they stood on %s at commit %.12s.", s.Repo, s.Taken, s.Commit),
		}
		seen := map[string]bool{}
		for _, gh := range s.Milestones {
			if name := strings.TrimSpace(gh.Title); name != "" && !seen[name] {
				seen[name] = true
				p.Milestones = append(p.Milestones, SuiteMilestone{Name: name, Description: gh.Description, DueOn: gh.DueOn, Closed: gh.Closed})
			}
		}
		for _, gh := range s.Issues {
			if gh.Milestone != "" && !seen[gh.Milestone] {
				seen[gh.Milestone] = true
				p.Milestones = append(p.Milestones, SuiteMilestone{Name: gh.Milestone})
			}
			p.Issues = append(p.Issues, planIssue(s, gh, people))
			blocked := issue.FormatKey(s.Key, gh.Number)
			for _, ref := range Dependencies(gh.Body) {
				key := s.Key
				if ref.Repo != "" {
					key = keyOf[strings.ToLower(ref.Repo)]
				}
				blocker := issue.FormatKey(key, ref.Number)
				pair := [2]string{min(blocker, blocked), max(blocker, blocked)}
				if key == "" || !known[blocker] || blocker == blocked || linked[pair] {
					continue
				}
				linked[pair] = true
				plan.Links = append(plan.Links, SuiteLink{Blocker: blocker, Blocked: blocked})
			}
		}
		plan.Projects = append(plan.Projects, p)
	}
	return plan
}

func planIssue(s Snapshot, gh GitHubIssue, people int) SuiteIssue {
	out := SuiteIssue{
		Number: gh.Number, Summary: summary(gh.Title), TypeName: issueType(gh),
		Description: markdown.Document(gh.Body, "https://github.com/"+s.Repo+"/blob/"+s.Commit+"/"),
		CreatedAt:   gh.CreatedAt, Milestone: gh.Milestone, URL: gh.URL, Labels: labels(gh.Labels),
		Reporter: int(gh.Number % int64(people)), Assignee: int((gh.Number + 1) % int64(people)),
	}
	if gh.Closed {
		out.Done = true
		out.ResolvedAt = gh.ClosedAt
		if out.ResolvedAt == nil {
			out.ResolvedAt = &gh.CreatedAt
		}
	}
	return out
}

// issueType is a story for an issue written as one, a bug for one labelled or titled so, and a task otherwise.
func issueType(gh GitHubIssue) string {
	if userStory.MatchString(gh.Body) {
		return bootstrap.TypeStory
	}
	for _, l := range gh.Labels {
		if strings.EqualFold(l, "bug") || strings.EqualFold(l, "type/bug") {
			return bootstrap.TypeBug
		}
	}
	if bugTitle.MatchString(gh.Title) {
		return bootstrap.TypeBug
	}
	return bootstrap.TypeTask
}

// summary keeps a title within what a summary holds.
func summary(title string) string {
	title = strings.TrimSpace(title)
	if utf8.RuneCountInString(title) <= issue.MaxSummary {
		return title
	}
	return string([]rune(title)[:issue.MaxSummary])
}

// labels makes GitHub's label names words a label here can be.
func labels(names []string) []string {
	var out []string
	for _, name := range names {
		name = labelSpace.ReplaceAllString(strings.TrimSpace(name), "-")
		if utf8.RuneCountInString(name) > label.MaxNameLength {
			name = string([]rune(name)[:label.MaxNameLength])
		}
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// labelHues are the palette's colours by hue, in degrees; gray is for the colours with too little of one.
var labelHues = []struct {
	color string
	hue   float64
}{{"red", 0}, {"orange", 25}, {"amber", 45}, {"green", 140}, {"teal", 175}, {"blue", 217}, {"purple", 270}, {"pink", 330}}

// labelGray is the saturation below which a GitHub colour reads as gray.
const labelGray = 0.18

// labelColor is the palette colour nearest GitHub's hex colour by hue, or the one picked from the name when it is unreadable.
func labelColor(hex, name string) string {
	v, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	if err != nil || len(strings.TrimPrefix(hex, "#")) != 6 {
		return label.ColorFor(name)
	}
	r, g, b := float64(v>>16&0xff)/255, float64(v>>8&0xff)/255, float64(v&0xff)/255
	hi, lo := max(r, g, b), min(r, g, b)
	if hi-lo < labelGray {
		return "gray"
	}
	var hue float64
	switch hi {
	case r:
		hue = 60 * (g - b) / (hi - lo)
	case g:
		hue = 60 * (2 + (b-r)/(hi-lo))
	default:
		hue = 60 * (4 + (r-g)/(hi-lo))
	}
	if hue < 0 {
		hue += 360
	}
	best, distance := "gray", 360.0
	for _, h := range labelHues {
		d := min(math.Abs(hue-h.hue), 360-math.Abs(hue-h.hue))
		if d < distance {
			best, distance = h.color, d
		}
	}
	return best
}

// IssueRef names an issue: Repo is "owner/name", or empty for the issue's own repository.
type IssueRef struct {
	Repo   string
	Number int64
}

// Dependencies reads what an issue body says it depends on, under its own heading or on a line of its own,
// the way Exciter's suite demo reads it.
func Dependencies(body string) []IssueRef {
	var (
		section string
		lines   []string
	)
	for line := range strings.SplitSeq(body, "\n") {
		if m := sectionHeading.FindStringSubmatch(line); m != nil {
			section = strings.ToLower(m[1])
			continue
		}
		if section == dependsSection {
			lines = append(lines, line)
		} else if m := dependsInline.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			lines = append(lines, m[1])
		}
	}
	var out []IssueRef
	seen := map[IssueRef]bool{}
	for _, line := range lines {
		for _, m := range issueReference.FindAllStringSubmatch(line, -1) {
			n, err := strconv.ParseInt(m[2], 10, 64)
			if err != nil || n <= 0 {
				continue
			}
			ref := IssueRef{Repo: m[1], Number: n}
			if !seen[ref] {
				seen[ref] = true
				out = append(out, ref)
			}
		}
	}
	return out
}
