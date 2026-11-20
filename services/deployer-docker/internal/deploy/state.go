package deploy

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strings"
	"time"
)

// Labels the deployer reads on a container (spec 0006, "Cross child contracts").
const (
	labelInstance = "io.github.racherom.modular-platform.instance"
	labelModule   = "io.github.racherom.modular-platform.module"
	labelJob      = "io.github.racherom.modular-platform.job"
	// jobWait marks the wait anchor mp_jobs (spec 0014, "Waiting"): it belongs to no instance.
	jobWait = "wait"
)

// Container is one container of a project as the engine sees it.
type Container struct {
	ID           string
	Labels       map[string]string
	Image        string // the image as the container was created with it, a tag or reference@digest
	ImageID      string // the ID of the image it runs
	Status       string // created, running, paused, restarting, removing, exited or dead
	Health       string // starting, healthy, unhealthy; empty without a healthcheck
	ExitCode     int
	RestartCount int
	StartedAt    time.Time
	FinishedAt   time.Time
}

// Docker lists the containers of a Compose project, stopped ones included, follows the die and
// destroy events of deployed containers, and reads, tags and resolves images (spec 0031).
type Docker interface {
	Containers(ctx context.Context, project string) ([]Container, error)
	// Events streams the die and destroy events of containers carrying the stack label,
	// from since on, until ctx ends or the stream fails; then the error channel gets one error.
	Events(ctx context.Context, since time.Time) (<-chan ExitEvent, <-chan error)
	// ImageInspect reads the image name names (a tag, reference@digest or an image ID), or
	// returns ErrImageNotFound.
	ImageInspect(ctx context.Context, name string) (Image, error)
	// ImageTag adds the tag target to the image source names.
	ImageTag(ctx context.Context, source, target string) error
	// ImageUntag removes the tag; Docker frees the image when nothing else holds it.
	ImageUntag(ctx context.Context, tag string) error
	// ImageTags lists every tag repository:<tag> on the host.
	ImageTags(ctx context.Context, repository string) ([]string, error)
	// DistributionDigest asks the registry which manifest digest ref names, without pulling. A
	// failure is a *ResolveError.
	DistributionDigest(ctx context.Context, ref string) (string, error)
}

// ModuleState is a module's state as the editor shows it.
type ModuleState string

// The module states the deployer reports; api adds missing.
const (
	StatePending    ModuleState = "pending"
	StateStarting   ModuleState = "starting"
	StateRunning    ModuleState = "running"
	StateUnhealthy  ModuleState = "unhealthy"
	StateRestarting ModuleState = "restarting"
	StateExited     ModuleState = "exited"
	StatePaused     ModuleState = "paused"
)

// Service is the state of one instance of a project.
type Service struct {
	Instance, Module, Image string
	State                   ModuleState
	Health                  string
	ExitCode                *int // set when exited
	LastExitCode            *int // the most recent exit, any cause; nil when unknown
	RestartCount            int
	Since                   time.Time
}

// ErrDocker reports that the container engine did not answer.
var ErrDocker = errors.New("deploy: docker did not answer")

// State reports each container of project, sorted by instance, and whether the project is
// present at all (a container or its folder exists). The wait anchor mp_jobs is left out: it
// belongs to no instance.
func (e *Engine) State(ctx context.Context, project string) (bool, []Service, error) {
	containers, err := e.docker.Containers(ctx, project)
	if err != nil {
		return false, nil, errors.Join(ErrDocker, err)
	}
	services := make([]Service, 0, len(containers))
	for _, c := range containers {
		if c.Labels[labelJob] == jobWait {
			continue
		}
		services = append(services, e.service(c))
	}
	slices.SortFunc(services, func(a, b Service) int { return strings.Compare(a.Instance, b.Instance) })
	present := len(containers) > 0
	if !present {
		_, err := os.Lstat(e.projectDir(project))
		present = !errors.Is(err, fs.ErrNotExist)
	}
	return present, services, nil
}

// service maps a container onto a module state (child 0007, "Module state"), with its last exit
// code: the current one while exited or restarting, else the latest die event seen.
func (e *Engine) service(c Container) Service {
	s := Service{
		Instance: c.Labels[labelInstance], Module: c.Labels[labelModule], Image: c.Image,
		Health: c.Health, RestartCount: c.RestartCount, Since: c.StartedAt,
	}
	switch c.Status {
	case "created":
		s.State = StatePending
	case "running":
		switch c.Health {
		case "starting":
			s.State = StateStarting
		case "unhealthy":
			s.State = StateUnhealthy
		default:
			s.State = StateRunning
		}
	case "restarting":
		code := c.ExitCode
		s.State, s.LastExitCode = StateRestarting, &code
	case "paused":
		s.State = StatePaused
	default: // exited, dead, removing
		code := c.ExitCode
		s.State, s.ExitCode, s.LastExitCode, s.Since = StateExited, &code, &code, c.FinishedAt
	}
	if s.LastExitCode == nil && c.Status != "created" {
		if code, ok := e.exits.last(c.ID); ok {
			s.LastExitCode = &code
		}
	}
	return s
}
