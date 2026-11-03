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
)

// Container is one container of a project as the engine sees it.
type Container struct {
	Labels       map[string]string
	Image        string
	Status       string // created, running, paused, restarting, removing, exited or dead
	Health       string // starting, healthy, unhealthy; empty without a healthcheck
	ExitCode     int
	RestartCount int
	StartedAt    time.Time
	FinishedAt   time.Time
}

// Docker lists the containers of a Compose project, stopped ones included.
type Docker interface {
	Containers(ctx context.Context, project string) ([]Container, error)
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
	RestartCount            int
	Since                   time.Time
}

// ErrDocker reports that the container engine did not answer.
var ErrDocker = errors.New("deploy: docker did not answer")

// State reports each container of project, sorted by instance, and whether the project is
// present at all (a container or its folder exists).
func (e *Engine) State(ctx context.Context, project string) (bool, []Service, error) {
	containers, err := e.docker.Containers(ctx, project)
	if err != nil {
		return false, nil, errors.Join(ErrDocker, err)
	}
	services := make([]Service, 0, len(containers))
	for _, c := range containers {
		services = append(services, service(c))
	}
	slices.SortFunc(services, func(a, b Service) int { return strings.Compare(a.Instance, b.Instance) })
	present := len(services) > 0
	if !present {
		_, err := os.Lstat(e.projectDir(project))
		present = !errors.Is(err, fs.ErrNotExist)
	}
	return present, services, nil
}

// service maps a container onto a module state (child 0007, "Module state").
func service(c Container) Service {
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
		s.State = StateRestarting
	case "paused":
		s.State = StatePaused
	default: // exited, dead, removing
		code := c.ExitCode
		s.State, s.ExitCode, s.Since = StateExited, &code, c.FinishedAt
	}
	return s
}
