package demo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/armature/armature/backend/internal/audit"
	"github.com/armature/armature/backend/internal/auth"
	"github.com/armature/armature/backend/internal/board"
	"github.com/armature/armature/backend/internal/db"
	"github.com/armature/armature/backend/internal/issue"
	"github.com/armature/armature/backend/internal/label"
	"github.com/armature/armature/backend/internal/milestone"
	"github.com/armature/armature/backend/internal/observability"
	"github.com/armature/armature/backend/internal/project"
	"github.com/armature/armature/backend/internal/report"
	"github.com/armature/armature/backend/internal/template"
	"github.com/armature/armature/backend/internal/workflow"
)

// The suite demo's states, as Exciter's suite seed reads them.
const (
	SuiteQueued  = "queued"
	SuiteRunning = "running"
	SuiteDone    = "done"
	SuiteFailed  = "failed"
)

// suitePoll is how often the worker looks for a suite demo somebody asked for.
const suitePoll = 2 * time.Second

// SuiteStale is how long a running suite demo may go without a step before its worker is taken for stopped.
// A step is one project's issues, which a worker writes in seconds.
const SuiteStale = 15 * time.Minute

// suiteAgain is how a stopped suite demo is put right: adding it again carries on in the projects it made.
const suiteAgain = "Add the suite demo again, and it carries on in the projects it made."

// ErrSuiteAdded refuses a second suite demo while the first is built or done.
var ErrSuiteAdded = errors.New("the suite demo was added already")

// SuiteKeyTakenError is a suite demo held off by a project already under one of its keys.
type SuiteKeyTakenError struct {
	Key string
}

func (e *SuiteKeyTakenError) Error() string {
	return fmt.Sprintf("the project key %s is taken already", e.Key)
}

// SuiteDemo is how far the suite demo got.
type SuiteDemo struct {
	State   string `json:"state"`
	Step    int    `json:"step"`
	Steps   int    `json:"steps"`
	Message string `json:"message"`
	// Taken is the day the snapshot was taken.
	Taken     string    `json:"taken"`
	Projects  []string  `json:"projects"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

const selectSuite = `SELECT state, step, steps, message, taken, created_at, updated_at FROM suite_demo`

func scanSuite(row pgx.Row) (*SuiteDemo, error) {
	out := &SuiteDemo{Projects: SuiteKeys}
	err := row.Scan(&out.State, &out.Step, &out.Steps, &out.Message, &out.Taken, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return out, err
}

// Suite is the suite demo, or nil when nobody added it.
func Suite(ctx context.Context, cluster *db.Cluster) (*SuiteDemo, error) {
	var out *SuiteDemo
	err := cluster.Read(db.PinPrimary(ctx), func(ctx context.Context, tx db.DBTX) error {
		var err error
		out, err = scanSuite(tx.QueryRow(ctx, selectSuite))
		return err
	})
	return out, err
}

// QueueSuite asks the worker for the suite demo, or for a failed one again. The keys are checked first,
// so a refusal leaves nothing made; a failed demo's own projects do not hold it off.
func QueueSuite(ctx context.Context, cluster *db.Cluster, p *auth.Principal, ip string) (*SuiteDemo, db.LSN, error) {
	ctx = db.PinPrimary(ctx)
	snapshots, err := Snapshots()
	if err != nil {
		return nil, 0, err
	}
	projects := project.NewService(cluster)
	var out *SuiteDemo
	lsn, err := cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		// The row lock makes two administrators asking at once one demo and one refusal.
		if _, err := tx.Exec(ctx, `LOCK TABLE suite_demo IN EXCLUSIVE MODE`); err != nil {
			return err
		}
		existing, err := scanSuite(tx.QueryRow(ctx, selectSuite))
		if err != nil {
			return err
		}
		if existing != nil && existing.State != SuiteFailed {
			return ErrSuiteAdded
		}
		var made []string
		if existing != nil {
			if err := tx.QueryRow(ctx, `SELECT made FROM suite_demo`).Scan(&made); err != nil {
				return err
			}
		}
		for _, key := range SuiteKeys {
			if slices.Contains(made, key) {
				continue
			}
			free, err := projects.KeyAvailable(ctx, key)
			if err != nil {
				return err
			}
			if !free {
				return &SuiteKeyTakenError{Key: key}
			}
		}
		out, err = scanSuite(tx.QueryRow(ctx, `
			INSERT INTO suite_demo (steps, taken, actor_id) VALUES ($1, $2, $3)
			ON CONFLICT (id) DO UPDATE SET state = $4, step = 0, steps = $1, message = '', taken = $2, actor_id = $3
			RETURNING state, step, steps, message, taken, created_at, updated_at`,
			SuiteSteps, snapshots[0].Taken, p.User.ID, SuiteQueued))
		if err != nil {
			return err
		}
		return audit.Write(ctx, tx, audit.Entry{
			Action: "installation.suite_demo_added", TargetType: "installation", Actor: p.User.ID, IP: ip,
			Data: map[string]any{"projects": SuiteKeys, "taken": snapshots[0].Taken, "again": existing != nil},
		})
	})
	if err != nil {
		return nil, 0, err
	}
	return out, lsn, nil
}

// SeedSuite builds the suite demo in the caller's process, or brings one built before up to the snapshot.
// It leaves keys somebody else's project holds alone.
func SeedSuite(ctx context.Context, cluster *db.Cluster, actor issue.Actor, people []uuid.UUID, log *slog.Logger) error {
	ctx = db.PinPrimary(ctx)
	existing, err := Suite(ctx, cluster)
	if err != nil {
		return err
	}
	snapshots, err := Snapshots()
	if err != nil {
		return err
	}
	if existing == nil {
		projects := project.NewService(cluster)
		for _, key := range SuiteKeys {
			free, err := projects.KeyAvailable(ctx, key)
			if err != nil {
				return err
			}
			if !free {
				log.Warn("a project holds one of the suite demo's keys, so the seed leaves the suite demo out", "key", key)
				return nil
			}
		}
	}
	nothing := suiteProgress{step: func(int) error { return nil }, made: func(string) error { return nil }}
	if err := buildSuite(ctx, cluster, snapshots, actor, people, nothing, log); err != nil {
		return err
	}
	_, err = cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO suite_demo (state, step, steps, taken, actor_id) VALUES ($1, $2, $2, $3, $4)
			ON CONFLICT (id) DO UPDATE SET state = $1, step = $2, steps = $2, message = '', taken = $3`,
			SuiteDone, SuiteSteps, snapshots[0].Taken, actor.UserID)
		return err
	})
	return err
}

// SuiteBuilder is the worker's side: it builds a suite demo somebody asked for.
type SuiteBuilder struct {
	db  *db.Cluster
	log *slog.Logger
}

// NewSuiteBuilder makes the builder.
func NewSuiteBuilder(cluster *db.Cluster, log *slog.Logger) *SuiteBuilder {
	return &SuiteBuilder{db: cluster, log: log}
}

// Run looks for a queued suite demo until the context ends.
func (b *SuiteBuilder) Run(ctx context.Context) error {
	ticker := time.NewTicker(suitePoll)
	defer ticker.Stop()
	for {
		if n, err := observability.Count(ctx, "suite-demo", b.Once); err != nil {
			b.log.Warn("suite demo failed", "error", err)
		} else if n > 0 {
			b.log.Info("suite demo built")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Once builds the suite demo when it is queued, and answers 1 when it did. A failure is the demo's, in its message.
// A build whose worker stopped is marked failed once it is stale, so it can be added again.
func (b *SuiteBuilder) Once(ctx context.Context) (int, error) {
	ctx = db.PinPrimary(ctx)
	var actorID *uuid.UUID
	claimed := false
	_, err := b.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		stale, err := tx.Exec(ctx, `
			UPDATE suite_demo SET state = $1, message = $2
			WHERE state = $3 AND updated_at < now() - make_interval(secs => $4)`,
			SuiteFailed, "The suite demo stopped: its worker stopped while building it. "+suiteAgain,
			SuiteRunning, SuiteStale.Seconds())
		if err != nil {
			return err
		}
		if stale.RowsAffected() > 0 {
			b.log.Warn("the suite demo's build went stale, so it is marked failed", "after", SuiteStale)
		}
		err = tx.QueryRow(ctx, `
			UPDATE suite_demo SET state = $1 WHERE state = $2
			RETURNING actor_id`, SuiteRunning, SuiteQueued).Scan(&actorID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		claimed = err == nil
		return err
	})
	if err != nil || !claimed {
		return 0, err
	}
	built := b.build(ctx, actorID)
	state, message := SuiteDone, ""
	if built != nil {
		b.log.Error("the suite demo stopped", "error", built)
		state, message = SuiteFailed, "The suite demo stopped: "+built.Error()+". Look at the worker's log for why. "+suiteAgain
	}
	_, err = b.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
		_, err := tx.Exec(ctx, `UPDATE suite_demo SET state = $1, message = $2`, state, message)
		return err
	})
	return 1, err
}

func (b *SuiteBuilder) build(ctx context.Context, actorID *uuid.UUID) error {
	if actorID == nil {
		return errors.New("the administrator who asked for it has left")
	}
	snapshots, err := Snapshots()
	if err != nil {
		return err
	}
	people, err := activeMembers(ctx, b.db, *actorID)
	if err != nil {
		return err
	}
	actor := issue.Actor{UserID: *actorID, OrgRole: auth.RoleAdmin}
	write := func(query string, arg any) error {
		_, err := b.db.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, query, arg)
			return err
		})
		return err
	}
	progress := suiteProgress{
		step: func(n int) error { return write(`UPDATE suite_demo SET step = $1`, n) },
		made: func(key string) error {
			return write(`UPDATE suite_demo SET made = array_append(made, $1::text) WHERE NOT $1::text = ANY (made)`, key)
		},
	}
	return buildSuite(ctx, b.db, snapshots, actor, people, progress, b.log)
}

// suiteProgress is what a build tells its row: the step it reached, and each project it made.
type suiteProgress struct {
	step func(int) error
	made func(key string) error
}

// activeMembers are the people who work the demo's issues: the installation's members, the caller first.
func activeMembers(ctx context.Context, cluster *db.Cluster, caller uuid.UUID) ([]uuid.UUID, error) {
	people := []uuid.UUID{caller}
	err := cluster.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		rows, err := tx.Query(ctx, `
			SELECT u.id FROM app_user u
			WHERE u.id <> $1 AND u.is_active AND u.erased_at IS NULL AND u.standing IN ($2, $3, $4)
			  AND u.id IS DISTINCT FROM (SELECT automation_user_id FROM installation)
			ORDER BY u.created_at, u.id`, caller, string(auth.RoleOwner), string(auth.RoleAdmin), string(auth.RoleMember))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			people = append(people, id)
		}
		return rows.Err()
	})
	return people, err
}

// buildSuite writes the plan: each project, its milestones and its issues, then the links between them.
// An issue imported before is found by its GitHub address and brought up to the snapshot.
func buildSuite(ctx context.Context, cluster *db.Cluster, snapshots []Snapshot, actor issue.Actor,
	people []uuid.UUID, progress suiteProgress, log *slog.Logger) error {
	if len(people) == 0 {
		people = []uuid.UUID{actor.UserID}
	}
	plan := PlanSuite(snapshots, len(people))
	store := workflow.NewStore()
	engine := workflow.NewEngine(workflow.NewDefaultRegistry())
	projects := project.NewService(cluster, board.Provisioner{}, report.Provisioner{})
	templates := template.NewService(cluster, projects, workflow.NewAdmin(cluster, store))
	issues := issue.NewService(cluster, engine, store)
	labels := label.NewService(cluster)
	milestones := milestone.NewService(cluster)
	types, err := issueTypes(ctx, cluster)
	if err != nil {
		return err
	}
	importer := actor
	importer.Import = true

	for n, p := range plan.Projects {
		made, err := suiteProject(ctx, projects, templates, p, actor.UserID, progress.made)
		if err != nil {
			return err
		}
		named, err := suiteMilestones(ctx, milestones, p, actor.UserID)
		if err != nil {
			return err
		}
		done, err := doneStatuses(ctx, cluster, store, made.ID, types)
		if err != nil {
			return err
		}
		for _, planned := range p.Issues {
			in := issue.ImportInput{
				CreateInput: issue.CreateInput{
					ProjectKey: p.Key, TypeID: types[planned.TypeName], Summary: planned.Summary,
					Description: planned.Description, AssigneeID: &people[planned.Assignee],
				},
				ExternalKey: planned.URL, KeyNum: &planned.Number, ReporterID: &people[planned.Reporter],
				CreatedAt: planned.CreatedAt, ResolvedAt: planned.ResolvedAt, Source: suiteSource,
			}
			if planned.Done {
				in.StatusID = done[in.TypeID]
				in.UpdatedAt = *planned.ResolvedAt
			}
			written, _, err := issues.Import(ctx, in, importer)
			if err != nil {
				return fmt.Errorf("import %s: %w", planned.Key(p.Key), err)
			}
			key := written.Issue.Key
			if key != planned.Key(p.Key) {
				log.Warn("a GitHub number was taken in the project, so the issue has another", "github", planned.URL, "key", key)
			}
			var ms *uuid.UUID
			if id, ok := named[planned.Milestone]; ok {
				ms = &id
			}
			// Like the import itself, filing the issue tells nobody.
			if _, err := cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
				if _, _, _, err := labels.SetIssueLabelsIn(ctx, tx, key, planned.Labels); err != nil {
					return err
				}
				_, _, err := issues.SetMilestoneIn(ctx, tx, key, ms)
				return err
			}); err != nil {
				return fmt.Errorf("file %s: %w", key, err)
			}
		}
		log.Info("suite demo project filled", "project", p.Key, "issues", len(p.Issues))
		if err := progress.step(n + 1); err != nil {
			return err
		}
	}
	added, err := suiteLinks(ctx, issues, plan.Links, actor, log)
	if err != nil {
		return err
	}
	log.Info("suite demo linked", "links", added)
	return progress.step(len(plan.Projects) + 1)
}

// suiteProject makes the project, or finds the one an earlier run made, and records the one it made.
func suiteProject(ctx context.Context, projects *project.Service, templates *template.Service, p SuiteProject, actor uuid.UUID,
	record func(key string) error) (*project.Project, error) {
	made, _, err := templates.Create(ctx, template.Default, project.CreateInput{
		Key: p.Key, Name: p.Name, Description: p.Description, LeadID: &actor,
	}, actor)
	if errors.Is(err, project.ErrKeyTaken) {
		return projects.ByKey(ctx, p.Key)
	}
	if err != nil {
		return nil, fmt.Errorf("create the %s project: %w", p.Key, err)
	}
	return made, record(p.Key)
}

// suiteMilestones makes the milestones the project lacks and answers all of them by name.
func suiteMilestones(ctx context.Context, milestones *milestone.Service, p SuiteProject, actor uuid.UUID) (map[string]uuid.UUID, error) {
	have, err := milestones.List(ctx, p.Key, true)
	if err != nil {
		return nil, err
	}
	named := map[string]uuid.UUID{}
	for _, m := range have {
		named[m.Name] = m.ID
	}
	for _, name := range p.Milestones {
		if _, ok := named[name]; ok {
			continue
		}
		made, _, err := milestones.Create(ctx, p.Key, milestone.Input{Name: name}, actor)
		if err != nil {
			return nil, fmt.Errorf("create milestone %s in %s: %w", name, p.Key, err)
		}
		named[name] = made.ID
	}
	return named, nil
}

// doneStatuses is, per issue type, the last done status of the workflow the project gives it.
func doneStatuses(ctx context.Context, cluster *db.Cluster, store *workflow.Store, projectID uuid.UUID, types map[string]uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	out := map[uuid.UUID]uuid.UUID{}
	err := cluster.Read(ctx, func(ctx context.Context, tx db.DBTX) error {
		for name, typeID := range types {
			wf, err := store.ForIssueType(ctx, tx, projectID, typeID)
			if err != nil {
				return fmt.Errorf("read the workflow for %s: %w", name, err)
			}
			for _, s := range wf.Steps {
				if s.Status.Category == workflow.CategoryDone {
					out[typeID] = s.Status.ID
				}
			}
		}
		return nil
	})
	return out, err
}

// suiteLinks adds the blocking links an issue's "Depends on" names, leaving those already there.
// A dependency that would make an issue block itself is left out and said in the log.
func suiteLinks(ctx context.Context, issues *issue.Service, links []SuiteLink, actor issue.Actor, log *slog.Logger) (int, error) {
	have := map[[2]string]bool{}
	read := map[string]bool{}
	added := 0
	for _, l := range links {
		if !read[l.Blocker] {
			read[l.Blocker] = true
			existing, err := issues.Links(ctx, l.Blocker)
			if err != nil {
				return added, fmt.Errorf("read the links of %s: %w", l.Blocker, err)
			}
			for _, e := range existing {
				have[[2]string{l.Blocker, e.Issue.Key}] = true
			}
		}
		if have[[2]string{l.Blocker, l.Blocked}] {
			continue
		}
		_, _, err := issues.AddLink(ctx, l.Blocker, issue.LinkInput{TypeName: issue.LinkTypeBlocks, TargetKey: l.Blocked}, actor)
		if errors.Is(err, issue.ErrLinkCycle) {
			log.Warn("a dependency would close a circle, so it is left out", "blocker", l.Blocker, "blocked", l.Blocked)
			continue
		}
		if err != nil {
			return added, fmt.Errorf("link %s to %s: %w", l.Blocker, l.Blocked, err)
		}
		added++
	}
	return added, nil
}

// SuiteKeysText names the suite demo's keys for a sentence.
func SuiteKeysText() string {
	return strings.Join(SuiteKeys[:len(SuiteKeys)-1], ", ") + " and " + SuiteKeys[len(SuiteKeys)-1]
}
