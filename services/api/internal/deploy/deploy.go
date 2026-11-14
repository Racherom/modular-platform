// Package deploy is api's stack record and orchestration (spec 0006): it reviews a graph,
// starts an apply through the deploy target, polls it to its end and keeps the history. A
// stack's state is derived from its latest operation, never stored. Persistence, the
// renderer and the deploy target sit behind the interfaces this package defines.
package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/target/deploytarget"
)

// ReservedName is the platform's own stack; no stack may take it.
const ReservedName = "modular-platform"

// Errors callers branch on.
var (
	ErrNotFound     = errors.New("deploy: no such stack")
	ErrBusy         = errors.New("deploy: an operation of this stack is running")
	ErrGraphChanged = errors.New("deploy: the check result or an image version changed since review")
	// ErrLockInvalid reports a stack whose image lock file the deploy target cannot read.
	ErrLockInvalid = errors.New("deploy: the deploy target cannot read the stack's image lock")
	// ErrAlreadyRemoved refuses a plain remove of a removed stack.
	ErrAlreadyRemoved = errors.New("deploy: the stack is removed already")
	// ErrRenderer reports a renderer that did not answer as its contract says.
	ErrRenderer = errors.New("deploy: the renderer failed")
	// ErrTargetUnavailable reports a deploy target that did not answer.
	ErrTargetUnavailable = errors.New("deploy: the deploy target did not answer")
	// ErrTarget reports a deploy target that answered outside its contract: a wiring fault.
	ErrTarget = errors.New("deploy: the deploy target answered unexpectedly")
)

// RefusedError lists why a review or apply was refused; nothing happened.
type RefusedError struct {
	Problems []contract.Problem
}

func (e *RefusedError) Error() string { return "deploy: refused" }

func refused(code, path, message string) *RefusedError {
	return &RefusedError{Problems: []contract.Problem{{Severity: contract.Error, Code: code, Path: path, Message: message}}}
}

// Action is what an operation does.
type Action string

// The actions.
const (
	ActionApply  Action = "apply"
	ActionRemove Action = "remove"
)

// Status is where an operation stands; every status but running is final.
type Status string

// The statuses.
const (
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusUnknown   Status = "unknown"
)

// Step is the last step an operation reached.
type Step string

// The steps.
const (
	StepPrepare Step = "prepare"
	StepPull    Step = "pull"
	StepUp      Step = "up"
	StepDown    Step = "down"
	StepDone    Step = "done"
)

// Starter is who started an operation, from their session.
type Starter struct {
	Issuer, Subject, Email string
}

// Operation is one row of a stack's history.
type Operation struct {
	ID                  uuid.UUID
	StackID             uuid.UUID
	Action              Action
	Status              Status
	Step                Step
	Graph               json.RawMessage // apply only
	Digest              string
	DeleteData          bool
	RollbackOf          *uuid.UUID
	PlacedSupplied      []string
	StartedBy           Starter
	DeployerOperationID string
	DeployerBootID      string
	Error               string
	StartedAt           time.Time
	FinishedAt          *time.Time
}

// Stack is a stack with its latest operation, if any.
type Stack struct {
	ID             uuid.UUID
	Name           string
	ComposeProject string
	CreatedAt      time.Time
	Latest         *Operation
}

// State is a stack's state, derived from its latest operation.
type State string

// The stack states.
const (
	StateApplying    State = "applying"
	StateRollingBack State = "rolling-back"
	StateRemoving    State = "removing"
	StateRunning     State = "running"
	StateRolledBack  State = "rolled-back"
	StateRemoved     State = "removed"
	StateFailed      State = "failed"
	StateUnknown     State = "unknown"
)

// State derives the stack's state from its latest operation (spec 0006, "State
// transitions"). A stack without one is applying: its first operation is being started.
func (d Stack) State() State {
	op := d.Latest
	if op == nil {
		return StateApplying
	}
	rollback := op.RollbackOf != nil
	switch {
	case op.Status == StatusRunning && op.Action == ActionRemove:
		return StateRemoving
	case op.Status == StatusRunning && rollback:
		return StateRollingBack
	case op.Status == StatusRunning:
		return StateApplying
	case op.Status == StatusSucceeded && op.Action == ActionRemove:
		return StateRemoved
	case op.Status == StatusSucceeded && rollback:
		return StateRolledBack
	case op.Status == StatusSucceeded:
		return StateRunning
	case op.Status == StatusFailed:
		return StateFailed
	default:
		return StateUnknown
	}
}

// ModuleState is the live state of one instance of a stack.
type ModuleState struct {
	Instance, Module, Image string
	State                   string // the deploy target's states plus missing
	Health                  string
	ExitCode                *int
	LastExitCode            *int
	RestartCount            int
	Since                   time.Time
}

// View is a stack with the graph it runs and the live state of its modules.
type View struct {
	Stack
	Graph            *contract.Graph // of the latest succeeded apply
	LatestApplyGraph *contract.Graph // of the newest apply, any status; Apply again reviews it
	RolledBackFrom   *Operation
	Modules          []ModuleState
	StateError       string
}

// PlacedCredential is a placed secret the review lists.
type PlacedCredential struct {
	Path, Instance, Capability, Field string
	Exists                            bool
}

// Review is what an apply of a graph would run. Digest covers the check result and the frozen
// image lock (spec 0031); apply sends it back.
type Review struct {
	Check          contract.CheckResult
	Digest         string
	Modules        []contract.ModuleImage
	Images         []ReviewImage
	PublishedPorts []contract.PublishedPort
	Placed         []PlacedCredential
}

// ReviewImage is one image reference the stack runs, frozen to the digest an apply runs.
// Available is the registry's digest when checkNewer asked and it differs; Present is false
// when the image is not on the host.
type ReviewImage struct {
	Reference      string
	UsedBy         []contract.ImageUser // the render's users without mp_jobs
	Digest         string
	Change         contract.ImageChange
	PreviousDigest string
	Available      string
	Present        bool
}

// ImageOptions are the review's and apply's image choices: CheckNewer asks the registry about
// every frozen reference, Update takes these references at their newest digest.
type ImageOptions struct {
	CheckNewer bool
	Update     []string
}

// PlacedInput is a value you supply for a placed secret.
type PlacedInput struct {
	Path    string
	Value   string
	Replace bool
}

// RunningOperation is a running operation with what polling it needs.
type RunningOperation struct {
	Operation
	StackName      string
	ComposeProject string
}

// Store keeps stacks and their history.
type Store interface {
	// EnsureStack returns the stack named name, creating it when missing.
	EnsureStack(ctx context.Context, name string) (id uuid.UUID, composeProject string, created bool, err error)
	// DropNewStack deletes a stack that has no operation yet.
	DropNewStack(ctx context.Context, id uuid.UUID) error
	// StartOperation inserts op as running and makes it the stack's latest, in one
	// transaction; ErrBusy when another runs.
	StartOperation(ctx context.Context, op Operation) (Operation, error)
	// StartOperationIfLatest starts op like StartOperation, but only while latest is still the
	// stack's latest operation; false when it is not, or another operation runs.
	StartOperationIfLatest(ctx context.Context, op Operation, latest uuid.UUID) (Operation, bool, error)
	// DropOperation deletes an operation the deploy target refused and restores the latest.
	DropOperation(ctx context.Context, op Operation) error
	AcceptOperation(ctx context.Context, id uuid.UUID, deployerID, bootID string) error
	// ProgressOperation records a running operation's step; false when nothing changed.
	ProgressOperation(ctx context.Context, id uuid.UUID, step Step) (bool, error)
	// FinishOperation gives a running operation its final status; false when it was final already.
	FinishOperation(ctx context.Context, id uuid.UUID, status Status, step Step, errText string) (bool, error)
	Operation(ctx context.Context, id uuid.UUID) (Operation, bool, error)
	RunningOperations(ctx context.Context) ([]RunningOperation, error)
	Stack(ctx context.Context, name string) (Stack, bool, error)
	// Stacks lists stacks sorted by name, after the name after.
	Stacks(ctx context.Context, after string, limit int) ([]Stack, error)
	// Operations lists a stack's history newest first, before the operation before.
	Operations(ctx context.Context, stackID uuid.UUID, before *uuid.UUID, limit int) ([]Operation, error)
	LatestSucceededApply(ctx context.Context, stackID uuid.UUID) (Operation, bool, error)
	LatestApplyReachedUp(ctx context.Context, stackID uuid.UUID) (Operation, bool, error)
	// LatestApply returns the newest apply of the stack, whatever its status.
	LatestApply(ctx context.Context, stackID uuid.UUID) (Operation, bool, error)
	// LatestSucceededBefore returns the newest succeeded operation started before failed.
	LatestSucceededBefore(ctx context.Context, stackID, failed uuid.UUID) (Operation, bool, error)
	// CountRollbacks counts the operations whose rollback_of is of.
	CountRollbacks(ctx context.Context, of uuid.UUID) (int, error)
}

// Renderer checks and renders graphs.
type Renderer interface {
	Check(ctx context.Context, g contract.Graph) (contract.CheckResult, error)
	// RenderManaged renders g for stack with its images frozen by lock (none when nil); a check
	// result with errors comes back as a *RefusedError.
	RenderManaged(ctx context.Context, g contract.Graph, stack uuid.UUID, lock []contract.ImagePin) (contract.RenderedStack, error)
}

// TargetOperation is an operation as the deploy target reports it, with the boot id it
// answered under.
type TargetOperation struct {
	deploytarget.Operation
	BootID string
}

// Target is the deploy target. Its errors: *RefusedError (422), ErrBusy (409),
// ErrTargetUnavailable (no answer, 5xx, timeout), ErrNotFound (404), ErrTarget (anything else).
type Target interface {
	Apply(ctx context.Context, composeProject string, req deploytarget.ApplyRequest) (TargetOperation, error)
	// Remove starts a remove; ErrNotFound when the stack has neither a folder nor a container.
	Remove(ctx context.Context, composeProject string, deleteData bool) (TargetOperation, error)
	// Operation reads an operation; a 404 is ErrNotFound with the boot id it answered under.
	Operation(ctx context.Context, id string) (TargetOperation, string, error)
	State(ctx context.Context, composeProject string) (deploytarget.StackState, error)
	Secrets(ctx context.Context, composeProject string) ([]string, error)
	// Images reads the stack's image locks (spec 0031); ErrBusy while an operation runs,
	// ErrLockInvalid for a broken lock file.
	Images(ctx context.Context, composeProject string) (deploytarget.StackImages, error)
	// Resolve asks the registry for the digest of each reference, without pulling.
	Resolve(ctx context.Context, references []string) (deploytarget.ResolveResult, error)
}
