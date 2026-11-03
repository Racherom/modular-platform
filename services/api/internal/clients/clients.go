// Package clients adapts api's two providers, the planner and the deploy target, to the
// interfaces of package deploy: generated contract clients over the sdk's binding clients, with
// each answer mapped onto deploy's errors.
package clients

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/racherom/modular-platform/libs/contract"
	"github.com/racherom/modular-platform/libs/target/deploytarget"
	"github.com/racherom/modular-platform/sdk/httpclient"
	"github.com/racherom/modular-platform/services/api/internal/deploy"
	"github.com/racherom/modular-platform/services/api/internal/plannerclient"
)

// bootHeader carries the deploy target's boot id on every answer.
const bootHeader = "MP-Boot-Id"

// Planner calls the planner through api's planner binding.
type Planner struct {
	c *plannerclient.ClientWithResponses
}

var _ deploy.Planner = (*Planner)(nil)

// NewPlanner returns the planner client over the binding's client.
func NewPlanner(b *httpclient.Client) (*Planner, error) {
	c, err := plannerclient.NewClientWithResponses(b.URL().String(), plannerclient.WithHTTPClient(b))
	if err != nil {
		return nil, fmt.Errorf("planner client: %w", err)
	}
	return &Planner{c: c}, nil
}

func plannerError(op string, status int, body []byte) error {
	return fmt.Errorf("%w: %s answered %d: %.200s", deploy.ErrPlanner, op, status, body)
}

// Catalog lists the catalog.
func (p *Planner) Catalog(ctx context.Context) (contract.Catalog, error) {
	resp, err := p.c.GetCatalogWithResponse(ctx)
	if err != nil {
		return contract.Catalog{}, errors.Join(deploy.ErrPlanner, err)
	}
	if resp.JSON200 == nil {
		return contract.Catalog{}, plannerError("catalog", resp.StatusCode(), resp.Body)
	}
	return *resp.JSON200, nil
}

// Plan plans g.
func (p *Planner) Plan(ctx context.Context, g contract.Graph) (contract.PlanResult, error) {
	resp, err := p.c.PlanWithResponse(ctx, g)
	if err != nil {
		return contract.PlanResult{}, errors.Join(deploy.ErrPlanner, err)
	}
	if resp.JSON400 != nil {
		return contract.PlanResult{}, &deploy.RefusedError{Problems: resp.JSON400.Problems}
	}
	if resp.JSON200 == nil {
		return contract.PlanResult{}, plannerError("plan", resp.StatusCode(), resp.Body)
	}
	return *resp.JSON200, nil
}

// Render answers the static zip of g as is, or the plan when it has errors.
func (p *Planner) Render(ctx context.Context, g contract.Graph) (zip []byte, disposition string, plan *contract.PlanResult, err error) {
	resp, err := p.c.RenderWithResponse(ctx, g)
	if err != nil {
		return nil, "", nil, errors.Join(deploy.ErrPlanner, err)
	}
	switch {
	case resp.StatusCode() == http.StatusOK:
		return resp.Body, resp.HTTPResponse.Header.Get("Content-Disposition"), nil, nil
	case resp.JSON422 != nil:
		return nil, "", resp.JSON422, nil
	case resp.JSON400 != nil:
		return nil, "", nil, &deploy.RefusedError{Problems: resp.JSON400.Problems}
	}
	return nil, "", nil, plannerError("render", resp.StatusCode(), resp.Body)
}

// RenderManaged renders g for the deployment; a plan with errors is a refusal.
func (p *Planner) RenderManaged(ctx context.Context, g contract.Graph, deployment uuid.UUID) (contract.RenderedProject, error) {
	resp, err := p.c.RenderManagedWithResponse(ctx, contract.ManagedRenderRequest{Graph: g, Deployment: deployment})
	if err != nil {
		return contract.RenderedProject{}, errors.Join(deploy.ErrPlanner, err)
	}
	switch {
	case resp.JSON200 != nil:
		return *resp.JSON200, nil
	case resp.JSON422 != nil:
		return contract.RenderedProject{}, &deploy.RefusedError{Problems: resp.JSON422.Problems}
	case resp.JSON400 != nil:
		return contract.RenderedProject{}, &deploy.RefusedError{Problems: resp.JSON400.Problems}
	}
	return contract.RenderedProject{}, plannerError("managed render", resp.StatusCode(), resp.Body)
}

// Target calls the deploy target through api's deploy-target binding.
type Target struct {
	c *deploytarget.ClientWithResponses
}

var _ deploy.Target = (*Target)(nil)

// NewTarget returns the deploy target client over the binding's client.
func NewTarget(b *httpclient.Client) (*Target, error) {
	c, err := deploytarget.NewClientWithResponses(b.URL().String(), deploytarget.WithHTTPClient(b))
	if err != nil {
		return nil, fmt.Errorf("deploy target client: %w", err)
	}
	return &Target{c: c}, nil
}

func targetError(op string, status int, body []byte) error {
	if status >= http.StatusInternalServerError {
		return fmt.Errorf("%w: %s answered %d", deploy.ErrTargetUnavailable, op, status)
	}
	return fmt.Errorf("%w: %s answered %d: %.200s", deploy.ErrTarget, op, status, body)
}

// Apply starts an apply of the project.
func (t *Target) Apply(ctx context.Context, project string, req deploytarget.ApplyRequest) (deploy.TargetOperation, error) {
	resp, err := t.c.ApplyProjectWithResponse(ctx, project, req)
	if err != nil {
		return deploy.TargetOperation{}, errors.Join(deploy.ErrTargetUnavailable, err)
	}
	switch {
	case resp.JSON202 != nil:
		return deploy.TargetOperation{Operation: *resp.JSON202, BootID: resp.HTTPResponse.Header.Get(bootHeader)}, nil
	case resp.JSON409 != nil:
		return deploy.TargetOperation{}, deploy.ErrBusy
	case resp.JSON422 != nil:
		return deploy.TargetOperation{}, &deploy.RefusedError{Problems: resp.JSON422.Problems}
	}
	return deploy.TargetOperation{}, targetError("apply", resp.StatusCode(), resp.Body)
}

// Operation reads an operation and the boot id it answered under.
func (t *Target) Operation(ctx context.Context, id string) (deploy.TargetOperation, string, error) {
	resp, err := t.c.GetOperationWithResponse(ctx, id)
	if err != nil {
		return deploy.TargetOperation{}, "", errors.Join(deploy.ErrTargetUnavailable, err)
	}
	boot := resp.HTTPResponse.Header.Get(bootHeader)
	switch {
	case resp.JSON200 != nil:
		return deploy.TargetOperation{Operation: *resp.JSON200, BootID: boot}, boot, nil
	case resp.JSON404 != nil:
		return deploy.TargetOperation{}, boot, deploy.ErrNotFound
	}
	return deploy.TargetOperation{}, "", targetError("operation", resp.StatusCode(), resp.Body)
}

// State reads the project's containers.
func (t *Target) State(ctx context.Context, project string) (deploytarget.ProjectState, error) {
	resp, err := t.c.GetProjectStateWithResponse(ctx, project)
	if err != nil {
		return deploytarget.ProjectState{}, errors.Join(deploy.ErrTargetUnavailable, err)
	}
	if resp.JSON200 == nil {
		return deploytarget.ProjectState{}, targetError("state", resp.StatusCode(), resp.Body)
	}
	return *resp.JSON200, nil
}

// Secrets lists the project's secret files that exist.
func (t *Target) Secrets(ctx context.Context, project string) ([]string, error) {
	resp, err := t.c.GetProjectSecretsWithResponse(ctx, project)
	if err != nil {
		return nil, errors.Join(deploy.ErrTargetUnavailable, err)
	}
	if resp.JSON200 == nil {
		return nil, targetError("secrets", resp.StatusCode(), resp.Body)
	}
	return resp.JSON200.Present, nil
}
