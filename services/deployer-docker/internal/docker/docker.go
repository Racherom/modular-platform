// Package docker holds the deployer's adapters to the container engine: the docker compose CLI
// plugin and the moby client. Nothing outside it names Docker types.
package docker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
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
	c := deploy.Container{ID: s.ID, Labels: s.Labels, Image: s.Image, ImageID: s.ImageID, Status: string(s.State), RestartCount: in.RestartCount}
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

// labelStack marks a deployed container; the platform's own services carry it too, with
// the value platform, but they are never in a deployed project's state read.
const labelStack = "io.github.racherom.modular-platform.stack"

// Events streams the die and destroy events of containers that carry the stack label, from
// since on. The error channel gets one error when the stream ends, ctx's error when ctx ended.
func (e *Engine) Events(ctx context.Context, since time.Time) (<-chan deploy.ExitEvent, <-chan error) {
	res := e.c.Events(ctx, client.EventsListOptions{
		Since: since.UTC().Format(time.RFC3339Nano),
		Filters: client.Filters{}.
			Add("type", string(events.ContainerEventType)).
			Add("event", string(events.ActionDie), string(events.ActionDestroy)).
			Add("label", labelStack),
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

// ImageInspect reads the image name names: a tag, reference@digest or an image ID.
func (e *Engine) ImageInspect(ctx context.Context, name string) (deploy.Image, error) {
	res, err := e.c.ImageInspect(ctx, name)
	if cerrdefs.IsNotFound(err) {
		return deploy.Image{}, errors.Join(deploy.ErrImageNotFound, err)
	}
	if err != nil {
		return deploy.Image{}, err
	}
	return deploy.Image{ID: res.ID, RepoDigests: res.RepoDigests}, nil
}

// ImageTag adds the tag target to the image source names.
func (e *Engine) ImageTag(ctx context.Context, source, target string) error {
	_, err := e.c.ImageTag(ctx, client.ImageTagOptions{Source: source, Target: target})
	return err
}

// ImageUntag removes tag. Docker deletes the image only when it was its last name and no
// container uses it; the deployer never removes an image any other way.
func (e *Engine) ImageUntag(ctx context.Context, tag string) error {
	_, err := e.c.ImageRemove(ctx, tag, client.ImageRemoveOptions{})
	if cerrdefs.IsNotFound(err) {
		return errors.Join(deploy.ErrImageNotFound, err)
	}
	if cerrdefs.IsConflict(err) {
		return nil // a container still uses the image: the tag stays until a later apply
	}
	return err
}

// ImageTags lists every tag repository:<tag> on the host.
func (e *Engine) ImageTags(ctx context.Context, repository string) ([]string, error) {
	res, err := e.c.ImageList(ctx, client.ImageListOptions{Filters: client.Filters{}.Add("reference", repository+":*")})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, img := range res.Items {
		for _, t := range img.RepoTags {
			if strings.HasPrefix(t, repository+":") {
				out = append(out, t)
			}
		}
	}
	return out, nil
}

// DistributionDigest asks the registry, through the daemon and its mirror and proxy settings,
// which manifest digest ref names; for an image built for several platforms, the index digest.
func (e *Engine) DistributionDigest(ctx context.Context, ref string) (string, error) {
	res, err := e.c.DistributionInspect(ctx, ref, client.DistributionInspectOptions{})
	if err == nil {
		return res.Descriptor.Digest.String(), nil
	}
	kind := deploy.ResolveOther
	switch {
	case client.IsErrConnectionFailed(err):
		kind = deploy.ResolveNoEngine
	case cerrdefs.IsNotFound(err):
		kind = deploy.ResolveNotFound
	case cerrdefs.IsUnauthorized(err), cerrdefs.IsPermissionDenied(err):
		kind = deploy.ResolveUnauthorized
	}
	return "", &deploy.ResolveError{Kind: kind, Err: err}
}
