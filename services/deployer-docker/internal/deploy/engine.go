// Package deploy applies rendered Compose projects to this host and reports their state (spec
// 0006, child 0007). It keeps running operations in memory only: one per project at a time,
// different projects in parallel. It never checks, never stores history and never decides a
// rollback; api does that. Docker sits behind the Compose and Docker interfaces.
package deploy

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// Limits of the in memory operation list.
const (
	keepFinished = time.Hour
	maxOps       = 1000
)

// ErrBusy reports that an operation of the project still runs.
var ErrBusy = errors.New("deploy: an operation of this project is running")

// Action is what an operation does.
type Action string

// The actions.
const (
	ActionApply  Action = "apply"
	ActionRemove Action = "remove"
)

// Status is where an operation stands.
type Status string

// The statuses; the last two are final.
const (
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

// Step is the last step an operation reached.
type Step string

// The steps: prepare, pull, up, done for an apply; prepare, down, done for a remove.
const (
	StepPrepare Step = "prepare"
	StepPull    Step = "pull"
	StepUp      Step = "up"
	StepDown    Step = "down"
	StepDone    Step = "done"
)

// Operation is one apply or remove, as the deployer reports it.
type Operation struct {
	ID, Project string
	Action      Action
	Status      Status
	Step        Step
	Error       string // a problem code, then at most the last 16 KB of Compose's output
	StartedAt   time.Time
	FinishedAt  time.Time // zero while running
}

// File is one file of a rendered project; the engine writes every one with mode 0644.
type File struct {
	Path string
	Data []byte
}

// SecretKind tells who makes a secret file.
type SecretKind string

// The secret kinds.
const (
	SecretGenerated SecretKind = "generated"
	SecretPlaced    SecretKind = "placed"
)

// Secret is a secret file the project mounts, at secrets/<instance>/<name>.
type Secret struct {
	Path string
	Kind SecretKind
}

// Placed is a value you supply for a placed secret. Replace lets it overwrite an existing file.
type Placed struct {
	Path    string
	Data    []byte
	Replace bool
}

// ApplyRequest is a rendered project to apply. Caller is the MP-Caller subject, for the log.
type ApplyRequest struct {
	Project, Stack string
	Files          []File
	Secrets        []Secret
	Placed         []Placed
	Caller         string
}

// Config is where and as whom the engine writes, how long Compose may take, and which host
// ports no project may publish.
type Config struct {
	Dir         string // the stacks folder; project p lives in Dir/p
	UID, GID    int    // owner of every file and folder the engine writes
	PullTimeout time.Duration
	WaitTimeout time.Duration
	// ResolveTimeout bounds one resolve call for all its references (spec 0031).
	ResolveTimeout time.Duration
	ReservedPorts  []int // the platform ingress's HTTP and HTTPS ports
}

// Compose runs `docker compose` for the project in dir and returns its combined output.
type Compose interface {
	Run(ctx context.Context, dir, project string, args ...string) ([]byte, error)
}

// Engine runs operations. Create it with New.
type Engine struct {
	cfg     Config
	compose Compose
	docker  Docker
	log     *slog.Logger
	//nolint:containedctx // the engine's lifetime: operations outlive their request and end with it
	ctx     context.Context
	bootID  string
	started time.Time
	now     func() time.Time
	exits   *exits

	mu   sync.Mutex
	ops  map[string]*Operation
	busy map[string]bool // project -> an operation runs
	wg   sync.WaitGroup
}

// New returns an engine whose operations run until ctx ends. It picks a new boot id, so a
// restarted deployer tells api it forgot its operations.
func New(ctx context.Context, cfg Config, compose Compose, docker Docker, log *slog.Logger) *Engine {
	return &Engine{
		cfg: cfg, compose: compose, docker: docker, log: log, ctx: ctx,
		bootID: newBootID(), started: time.Now(), now: time.Now, exits: newExits(),
		ops: map[string]*Operation{}, busy: map[string]bool{},
	}
}

// BootID is chosen at each start: 16 random bytes, base64url.
func (e *Engine) BootID() string { return e.bootID }

// Wait blocks until every started operation has finished, for tests and shutdown.
func (e *Engine) Wait() { e.wg.Wait() }

// Operation returns a copy of the operation with id, if this boot knows it.
func (e *Engine) Operation(id string) (Operation, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	op, ok := e.ops[id]
	if !ok {
		return Operation{}, false
	}
	return *op, true
}

// start registers a running operation of project, or returns ErrBusy.
func (e *Engine) start(project string, action Action) (*Operation, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.busy[project] {
		return nil, ErrBusy
	}
	e.prune()
	op := &Operation{
		ID: newID(), Project: project, Action: action,
		Status: StatusRunning, Step: StepPrepare, StartedAt: e.now().UTC(),
	}
	e.ops[op.ID] = op
	e.busy[project] = true
	return op, nil
}

func (e *Engine) step(op *Operation, s Step) {
	e.mu.Lock()
	defer e.mu.Unlock()
	op.Step = s
}

// finish sets op's final status and frees its project.
func (e *Engine) finish(op *Operation, err error) Operation {
	e.mu.Lock()
	defer e.mu.Unlock()
	op.Status, op.FinishedAt = StatusSucceeded, e.now().UTC()
	if err != nil {
		op.Status, op.Error = StatusFailed, err.Error()
	} else {
		op.Step = StepDone
	}
	delete(e.busy, op.Project)
	return *op
}

// prune drops finished operations older than an hour, then the oldest finished ones while more
// than maxOps are kept. The caller holds mu.
func (e *Engine) prune() {
	cutoff := e.now().Add(-keepFinished)
	var finished []*Operation
	for id, op := range e.ops {
		switch {
		case op.Status == StatusRunning:
		case op.FinishedAt.Before(cutoff):
			delete(e.ops, id)
		default:
			finished = append(finished, op)
		}
	}
	if len(e.ops) < maxOps {
		return
	}
	slices.SortFunc(finished, func(a, b *Operation) int { return a.FinishedAt.Compare(b.FinishedAt) })
	for _, op := range finished[:min(len(finished), len(e.ops)-maxOps+1)] {
		delete(e.ops, op.ID)
	}
}

func newBootID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand never fails on supported platforms
	return base64.RawURLEncoding.EncodeToString(b)
}

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
