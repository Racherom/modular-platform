// Package docker holds the deployer's adapters to the container engine: the docker compose CLI
// plugin and the moby client. Nothing outside it names Docker types.
package docker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"

	"github.com/racherom/modular-platform/services/deployer-docker/internal/deploy"
)

// composeEnv is the whole env of a docker compose run. Compose reads the process env before the
// project's env files, so the deployer's own settings must never reach a deployed project.
var composeEnv = []string{
	"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	"HOME=/root",
}

// Compose runs the docker compose CLI plugin.
type Compose struct{}

var _ deploy.Compose = Compose{}

// Run runs docker compose for project in dir with its .env and deploy.env, and returns the
// combined output. Without a docker-compose.yml in dir it names the project alone, which is
// enough for down to find its containers by their labels.
func (Compose) Run(ctx context.Context, dir, project string, args ...string) ([]byte, error) {
	base := []string{"compose", "--project-name", project, "--progress", "plain"}
	file := filepath.Join(dir, "docker-compose.yml")
	_, statErr := os.Stat(file)
	if statErr == nil {
		base = append(base, "--project-directory", dir, "--file", file,
			"--env-file", filepath.Join(dir, ".env"), "--env-file", filepath.Join(dir, "deploy.env"))
	}
	cmd := exec.CommandContext(ctx, "docker", append(base, args...)...) //nolint:gosec // fixed binary; project and dir are checked by deploy
	if statErr == nil {
		cmd.Dir = dir
	}
	cmd.Env = composeEnv
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("docker compose %s: %w", args[0], err)
	}
	return out, nil
}

// Engine reads containers through the moby client.
type Engine struct {
	c *client.Client
}

var _ deploy.Docker = (*Engine)(nil)

// New connects to the engine named by the standard DOCKER_HOST settings, by default the socket.
func New() (*Engine, error) {
	c, err := client.New(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &Engine{c: c}, nil
}

// Close closes the client.
func (e *Engine) Close() error { return e.c.Close() }

// Containers lists every container of the Compose project, stopped ones included, with the
// restart count and health from an inspect of each.
func (e *Engine) Containers(ctx context.Context, project string) ([]deploy.Container, error) {
	list, err := e.c.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: client.Filters{}.Add("label", "com.docker.compose.project="+project),
	})
	if err != nil {
		return nil, err
	}
	out := make([]deploy.Container, 0, len(list.Items))
	for _, s := range list.Items {
		res, err := e.c.ContainerInspect(ctx, s.ID, client.ContainerInspectOptions{})
		if err != nil {
			return nil, err
		}
		out = append(out, toContainer(s, res.Container))
	}
	return out, nil
}

func toContainer(s container.Summary, in container.InspectResponse) deploy.Container {
	c := deploy.Container{ID: s.ID, Labels: s.Labels, Image: s.Image, Status: string(s.State), RestartCount: in.RestartCount}
	if st := in.State; st != nil {
		c.Status = string(st.Status)
		c.ExitCode = st.ExitCode
		c.StartedAt = parseTime(st.StartedAt)
		c.FinishedAt = parseTime(st.FinishedAt)
		if st.Health != nil {
			c.Health = string(st.Health.Status)
		}
	}
	return c
}

// labelDeployment marks a deployed container; the platform's own services carry it too, with
// the value platform, but they are never in a deployed project's state read.
const labelDeployment = "io.github.racherom.modular-platform.deployment"

// Events streams the die and destroy events of containers that carry the deployment label, from
// since on. The error channel gets one error when the stream ends, ctx's error when ctx ended.
func (e *Engine) Events(ctx context.Context, since time.Time) (<-chan deploy.ExitEvent, <-chan error) {
	res := e.c.Events(ctx, client.EventsListOptions{
		Since: since.UTC().Format(time.RFC3339Nano),
		Filters: client.Filters{}.
			Add("type", string(events.ContainerEventType)).
			Add("event", string(events.ActionDie), string(events.ActionDestroy)).
			Add("label", labelDeployment),
	})
	out := make(chan deploy.ExitEvent)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case m, ok := <-res.Messages:
				if !ok {
					return
				}
				ev := deploy.ExitEvent{
					ID: m.Actor.ID, Action: string(m.Action), ExitCode: m.Actor.Attributes["exitCode"],
					Time: time.Unix(0, m.TimeNano),
				}
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, res.Err
}

// parseTime reads Docker's RFC 3339 times; its zero value 0001-01-01 stays zero.
func parseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || t.Year() < 2000 {
		return time.Time{}
	}
	return t
}
