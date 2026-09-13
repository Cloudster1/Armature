package workflow

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
)

// Subject is the issue a transition is being evaluated against.
type Subject struct {
	IssueID     uuid.UUID
	ProjectID   uuid.UUID
	IssueTypeID uuid.UUID
	StatusID    uuid.UUID
	AssigneeID  *uuid.UUID
	ReporterID  *uuid.UUID
	HasParent   bool
}

// Actor is whoever is attempting the transition.
type Actor struct {
	UserID  uuid.UUID
	OrgRole string
}

// Input is what the user supplied along with the transition.
type Input struct {
	// Comment is optional prose recorded with the transition.
	Comment string
	// Assignee, when set, reassigns as part of the transition. A pointer to the
	// nil uuid means "unassign", which a plain nil cannot express.
	Assignee *uuid.UUID
	// Resolution names why the issue is being closed, when the workflow asks
	// for one.
	Resolution string
}

// Effect is what a transition's post-functions want done to the issue, over and
// above the status change itself. The caller applies it inside its own
// transaction, so rules never touch the database.
type Effect struct {
	// Assignee is set when a post-function reassigns the issue.
	Assignee    *uuid.UUID
	ClearAssign bool
	// Resolved is set when the issue should be marked resolved or unresolved.
	Resolved *bool
	// Comments are added to the issue, in order.
	Comments []string
}

// SetAssignee records a reassignment, overriding any earlier one.
func (e *Effect) SetAssignee(id uuid.UUID) {
	e.Assignee = &id
	e.ClearAssign = false
}

// ClearAssignee records an unassignment.
func (e *Effect) ClearAssignee() {
	e.Assignee = nil
	e.ClearAssign = true
}

// Condition decides whether a transition is offered.
type Condition interface {
	// Allow returns false and a reason when the transition should be hidden.
	// The reason is for administrators debugging a workflow, not for users.
	Allow(Subject, Actor) (bool, string)
}

// Validator checks the input of a chosen transition.
type Validator interface {
	// Validate returns a message the user can act on, or nil.
	Validate(Subject, Actor, Input) error
}

// PostFunction runs after the status change and records what else should happen.
type PostFunction interface {
	Apply(Subject, Actor, Input, *Effect) error
}

// Registry maps rule type names to their implementations. New rule types are
// added here rather than in the schema, so a workflow gains a capability
// without a migration.
type Registry struct {
	conditions    map[string]func(json.RawMessage) (Condition, error)
	validators    map[string]func(json.RawMessage) (Validator, error)
	postFunctions map[string]func(json.RawMessage) (PostFunction, error)
	described     map[string]RuleType
}

func NewRegistry() *Registry {
	return &Registry{
		conditions:    map[string]func(json.RawMessage) (Condition, error){},
		validators:    map[string]func(json.RawMessage) (Validator, error){},
		postFunctions: map[string]func(json.RawMessage) (PostFunction, error){},
		described:     map[string]RuleType{},
	}
}

// OptionKind says what kind of control a rule option wants.
type OptionKind string

const (
	// OptionText is free text.
	OptionText OptionKind = "text"
	// OptionRoles is one or more of the organization's roles.
	OptionRoles OptionKind = "roles"
)

// Option is one configurable value of a rule type, described for the designer.
type Option struct {
	Name     string     `json:"name"`
	Label    string     `json:"label"`
	Kind     OptionKind `json:"kind"`
	Required bool       `json:"required"`
	// Choices are the values a roles option may take, so the designer offers
	// the roles the engine compares against rather than guessing at them.
	Choices []string `json:"choices"`
}

// RuleType describes one registered rule for the designer's rule picker: what
// it is called, what it does, and what it needs configured.
type RuleType struct {
	Kind        RuleKind `json:"kind"`
	Type        string   `json:"type"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
	Options     []Option `json:"options"`
}

// Describe records how a rule type presents itself to people.
func (r *Registry) Describe(rt RuleType) {
	if rt.Options == nil {
		rt.Options = []Option{}
	}
	for i := range rt.Options {
		if rt.Options[i].Choices == nil {
			rt.Options[i].Choices = []string{}
		}
	}
	r.described[rt.Type] = rt
}

// Catalogue lists every registered rule type with its description, conditions
// first, then validators, then post-functions, each alphabetically by label.
func (r *Registry) Catalogue() []RuleType {
	out := []RuleType{}
	for _, kind := range []RuleKind{KindCondition, KindValidator, KindPostFunction} {
		for _, name := range r.RuleTypes(kind) {
			rt, ok := r.described[name]
			if !ok {
				rt = RuleType{Kind: kind, Type: name, Label: name, Options: []Option{}}
			}
			out = append(out, rt)
		}
	}
	return out
}

// Check builds the rule once, so a bad configuration is reported when the
// workflow is saved rather than when somebody takes the transition.
func (r *Registry) Check(kind RuleKind, ruleType string, config json.RawMessage) error {
	rule := Rule{Kind: kind, Type: ruleType, Config: config}
	var err error
	switch kind {
	case KindCondition:
		_, err = r.buildCondition(rule)
	case KindValidator:
		_, err = r.buildValidator(rule)
	case KindPostFunction:
		_, err = r.buildPostFunction(rule)
	default:
		return fmt.Errorf("unknown rule kind %q", kind)
	}
	return err
}

func (r *Registry) RegisterCondition(name string, build func(json.RawMessage) (Condition, error)) {
	r.conditions[name] = build
}

func (r *Registry) RegisterValidator(name string, build func(json.RawMessage) (Validator, error)) {
	r.validators[name] = build
}

func (r *Registry) RegisterPostFunction(name string, build func(json.RawMessage) (PostFunction, error)) {
	r.postFunctions[name] = build
}

// Known reports whether a rule type exists, so that a workflow can be validated
// when it is saved rather than failing the first time somebody uses it.
func (r *Registry) Known(kind RuleKind, ruleType string) bool {
	switch kind {
	case KindCondition:
		_, ok := r.conditions[ruleType]
		return ok
	case KindValidator:
		_, ok := r.validators[ruleType]
		return ok
	case KindPostFunction:
		_, ok := r.postFunctions[ruleType]
		return ok
	}
	return false
}

// RuleTypes lists the registered types of one kind, for the workflow editor.
func (r *Registry) RuleTypes(kind RuleKind) []string {
	var out []string
	switch kind {
	case KindCondition:
		for name := range r.conditions {
			out = append(out, name)
		}
	case KindValidator:
		for name := range r.validators {
			out = append(out, name)
		}
	case KindPostFunction:
		for name := range r.postFunctions {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

func (r *Registry) buildCondition(rule Rule) (Condition, error) {
	build, ok := r.conditions[rule.Type]
	if !ok {
		return nil, fmt.Errorf("unknown condition %q", rule.Type)
	}
	return build(rule.Config)
}

func (r *Registry) buildValidator(rule Rule) (Validator, error) {
	build, ok := r.validators[rule.Type]
	if !ok {
		return nil, fmt.Errorf("unknown validator %q", rule.Type)
	}
	return build(rule.Config)
}

func (r *Registry) buildPostFunction(rule Rule) (PostFunction, error) {
	build, ok := r.postFunctions[rule.Type]
	if !ok {
		return nil, fmt.Errorf("unknown post-function %q", rule.Type)
	}
	return build(rule.Config)
}

// ------------------------------------------------------- built-in rules ----

// Rule type names. They are stored in the database, so they are part of the
// data format and must not be renamed casually.
const (
	ConditionOrgRole    = "condition.org_role"
	ConditionIsAssignee = "condition.is_assignee"
	ConditionIsReporter = "condition.is_reporter"

	ValidatorAssigneeRequired   = "validator.assignee_required"
	ValidatorCommentRequired    = "validator.comment_required"
	ValidatorResolutionRequired = "validator.resolution_required"

	PostAssignToActor = "postfunction.assign_to_actor"
	PostClearAssignee = "postfunction.clear_assignee"
	PostSetResolved   = "postfunction.set_resolved"
	PostClearResolved = "postfunction.clear_resolved"
	PostAddComment    = "postfunction.add_comment"
)

// OrgRoles are the organization roles an actor can hold, as the org_role
// condition compares them. They mirror auth.OrgRole, which this package cannot
// import without a cycle; a test in httpapi holds the two lists together.
var OrgRoles = []string{"owner", "admin", "member", "customer"}

// NewDefaultRegistry returns a registry with the built-in rules installed.
func NewDefaultRegistry() *Registry {
	r := NewRegistry()

	r.Describe(RuleType{Kind: KindCondition, Type: ConditionOrgRole, Label: "Actor holds a role",
		Description: "Offered only to people whose organization role is one of these.",
		Options:     []Option{{Name: "roles", Label: "Roles", Kind: OptionRoles, Required: true, Choices: OrgRoles}}})
	r.Describe(RuleType{Kind: KindCondition, Type: ConditionIsAssignee, Label: "Actor is the assignee",
		Description: "Offered only to whoever the issue is assigned to."})
	r.Describe(RuleType{Kind: KindCondition, Type: ConditionIsReporter, Label: "Actor is the reporter",
		Description: "Offered only to whoever reported the issue."})
	r.Describe(RuleType{Kind: KindValidator, Type: ValidatorAssigneeRequired, Label: "Assignee required",
		Description: "The issue has to be assigned to somebody, before or as part of the move."})
	r.Describe(RuleType{Kind: KindValidator, Type: ValidatorCommentRequired, Label: "Comment required",
		Description: "The move has to come with a comment."})
	r.Describe(RuleType{Kind: KindValidator, Type: ValidatorResolutionRequired, Label: "Resolution required",
		Description: "The move has to say why the issue is being closed."})
	r.Describe(RuleType{Kind: KindPostFunction, Type: PostAssignToActor, Label: "Assign to the actor",
		Description: "Whoever makes the move becomes the assignee."})
	r.Describe(RuleType{Kind: KindPostFunction, Type: PostClearAssignee, Label: "Clear the assignee",
		Description: "The issue is left unassigned."})
	r.Describe(RuleType{Kind: KindPostFunction, Type: PostSetResolved, Label: "Mark resolved",
		Description: "The issue is marked resolved."})
	r.Describe(RuleType{Kind: KindPostFunction, Type: PostClearResolved, Label: "Mark unresolved",
		Description: "The issue is marked unresolved again."})
	r.Describe(RuleType{Kind: KindPostFunction, Type: PostAddComment, Label: "Add a comment",
		Description: "A fixed comment is added to the issue.",
		Options:     []Option{{Name: "text", Label: "Comment", Kind: OptionText, Required: true}}})

	r.RegisterCondition(ConditionOrgRole, func(config json.RawMessage) (Condition, error) {
		var cfg struct {
			Roles []string `json:"roles"`
		}
		if err := decode(config, &cfg); err != nil {
			return nil, err
		}
		if len(cfg.Roles) == 0 {
			return nil, fmt.Errorf("%s needs at least one role", ConditionOrgRole)
		}
		return orgRoleCondition{roles: cfg.Roles}, nil
	})

	r.RegisterCondition(ConditionIsAssignee, func(json.RawMessage) (Condition, error) {
		return isAssigneeCondition{}, nil
	})

	r.RegisterCondition(ConditionIsReporter, func(json.RawMessage) (Condition, error) {
		return isReporterCondition{}, nil
	})

	r.RegisterValidator(ValidatorAssigneeRequired, func(json.RawMessage) (Validator, error) {
		return assigneeRequired{}, nil
	})

	r.RegisterValidator(ValidatorCommentRequired, func(json.RawMessage) (Validator, error) {
		return commentRequired{}, nil
	})

	r.RegisterValidator(ValidatorResolutionRequired, func(json.RawMessage) (Validator, error) {
		return resolutionRequired{}, nil
	})

	r.RegisterPostFunction(PostAssignToActor, func(json.RawMessage) (PostFunction, error) {
		return assignToActor{}, nil
	})

	r.RegisterPostFunction(PostClearAssignee, func(json.RawMessage) (PostFunction, error) {
		return clearAssignee{}, nil
	})

	r.RegisterPostFunction(PostSetResolved, func(json.RawMessage) (PostFunction, error) {
		return setResolved{resolved: true}, nil
	})

	r.RegisterPostFunction(PostClearResolved, func(json.RawMessage) (PostFunction, error) {
		return setResolved{resolved: false}, nil
	})

	r.RegisterPostFunction(PostAddComment, func(config json.RawMessage) (PostFunction, error) {
		var cfg struct {
			Text string `json:"text"`
		}
		if err := decode(config, &cfg); err != nil {
			return nil, err
		}
		if strings.TrimSpace(cfg.Text) == "" {
			return nil, fmt.Errorf("%s needs some text", PostAddComment)
		}
		return addComment{text: cfg.Text}, nil
	})

	return r
}

// decode reads a rule's configuration, treating an absent one as empty rather
// than as a failure, so rules with no options need no config at all.
func decode(config json.RawMessage, into any) error {
	if len(config) == 0 || string(config) == "null" {
		return nil
	}
	if err := json.Unmarshal(config, into); err != nil {
		return fmt.Errorf("invalid rule configuration: %w", err)
	}
	return nil
}

type orgRoleCondition struct{ roles []string }

func (c orgRoleCondition) Allow(_ Subject, actor Actor) (bool, string) {
	if slices.Contains(c.roles, actor.OrgRole) {
		return true, ""
	}
	return false, fmt.Sprintf("requires one of these roles: %s", strings.Join(c.roles, ", "))
}

type isAssigneeCondition struct{}

func (isAssigneeCondition) Allow(subject Subject, actor Actor) (bool, string) {
	if subject.AssigneeID != nil && *subject.AssigneeID == actor.UserID {
		return true, ""
	}
	return false, "only the assignee can take this transition"
}

type isReporterCondition struct{}

func (isReporterCondition) Allow(subject Subject, actor Actor) (bool, string) {
	if subject.ReporterID != nil && *subject.ReporterID == actor.UserID {
		return true, ""
	}
	return false, "only the reporter can take this transition"
}

type assigneeRequired struct{}

func (assigneeRequired) Validate(subject Subject, _ Actor, input Input) error {
	// The input's assignee counts: assigning and transitioning in one step is
	// the normal way to start work on an unassigned issue.
	if input.Assignee != nil && *input.Assignee != uuid.Nil {
		return nil
	}
	if input.Assignee == nil && subject.AssigneeID != nil {
		return nil
	}
	return &RuleError{Field: "assignee", Message: "This transition needs an assignee."}
}

type commentRequired struct{}

func (commentRequired) Validate(_ Subject, _ Actor, input Input) error {
	if strings.TrimSpace(input.Comment) == "" {
		return &RuleError{Field: "comment", Message: "This transition needs a comment."}
	}
	return nil
}

type resolutionRequired struct{}

func (resolutionRequired) Validate(_ Subject, _ Actor, input Input) error {
	if strings.TrimSpace(input.Resolution) == "" {
		return &RuleError{Field: "resolution", Message: "Choose a resolution before closing this."}
	}
	return nil
}

type assignToActor struct{}

func (assignToActor) Apply(_ Subject, actor Actor, _ Input, effect *Effect) error {
	effect.SetAssignee(actor.UserID)
	return nil
}

type clearAssignee struct{}

func (clearAssignee) Apply(_ Subject, _ Actor, _ Input, effect *Effect) error {
	effect.ClearAssignee()
	return nil
}

type setResolved struct{ resolved bool }

func (s setResolved) Apply(_ Subject, _ Actor, _ Input, effect *Effect) error {
	value := s.resolved
	effect.Resolved = &value
	return nil
}

type addComment struct{ text string }

func (a addComment) Apply(_ Subject, _ Actor, _ Input, effect *Effect) error {
	effect.Comments = append(effect.Comments, a.text)
	return nil
}

// RuleError is a validation failure a user can act on. It carries the field it
// belongs to so the client can show it in the right place.
type RuleError struct {
	Field   string
	Message string
}

func (e *RuleError) Error() string { return e.Message }
