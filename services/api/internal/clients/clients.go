// Package clients adapts api's two providers, the renderer and the deploy target, to the
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
	"github.com/racherom/modular-platform/services/api/internal/rendererclient"
)

// bootHeader carries the deploy target's boot id on every answer.
const bootHeader = "MP-Boot-Id"

// Renderer calls the renderer through api's renderer binding.
type Renderer struct {
	c *rendererclient.ClientWithResponses
}

var _ deploy.Renderer = (*Renderer)(nil)

// NewRenderer returns the renderer client over the binding's client.
func NewRenderer(b *httpclient.Client) (*Renderer, error) {
	c, err := rendererclient.NewClientWithResponses(b.URL().String(), rendererclient.WithHTTPClient(b))
	if err != nil {
		return nil, fmt.Errorf("renderer client: %w", err)
	}
	return &Renderer{c: c}, nil
}

func rendererError(op string, status int, body []byte) error {
	return fmt.Errorf("%w: %s answered %d: %.200s", deploy.ErrRenderer, op, status, body)
}

// Catalog lists the catalog.
func (p *Renderer) Catalog(ctx context.Context) (contract.Catalog, error) {
	resp, err := p.c.GetCatalogWithResponse(ctx)
	if err != nil {
		return contract.Catalog{}, errors.Join(deploy.ErrRenderer, err)
	}
	if resp.JSON200 == nil {
		return contract.Catalog{}, rendererError("catalog", resp.StatusCode(), resp.Body)
	}
	return *resp.JSON200, nil
}

// Check checks g.
func (p *Renderer) Check(ctx context.Context, g contract.Graph) (contract.CheckResult, error) {
	resp, err := p.c.CheckWithResponse(ctx, g)
	if err != nil {
		return contract.CheckResult{}, errors.Join(deploy.ErrRenderer, err)
	}
	if resp.JSON400 != nil {
		return contract.CheckResult{}, &deploy.RefusedError{Problems: resp.JSON400.Problems}
	}
	if resp.JSON200 == nil {
		return contract.CheckResult{}, rendererError("check", resp.StatusCode(), resp.Body)
	}
	return *resp.JSON200, nil
}

// Render answers the static zip of g as is, or the check result when it has errors.
func (p *Renderer) Render(ctx context.Context, g contract.Graph) (zip []byte, disposition string, result *contract.CheckResult, err error) {
	resp, err := p.c.RenderWithResponse(ctx, g)
	if err != nil {
		return nil, "", nil, errors.Join(deploy.ErrRenderer, err)
	}
	switch {
	case resp.StatusCode() == http.StatusOK:
		return resp.Body, resp.HTTPResponse.Header.Get("Content-Disposition"), nil, nil
	case resp.JSON422 != nil:
		return nil, "", resp.JSON422, nil
	case resp.JSON400 != nil:
		return nil, "", nil, &deploy.RefusedError{Problems: resp.JSON400.Problems}
	}
	return nil, "", nil, rendererError("render", resp.StatusCode(), resp.Body)
}

// RenderManaged renders g for the stack with its images frozen by lock; a check result with
// errors, or a lock the renderer refuses, is a refusal.
func (p *Renderer) RenderManaged(ctx context.Context, g contract.Graph, stack uuid.UUID, lock []contract.ImagePin) (contract.RenderedStack, error) {
	resp, err := p.c.RenderManagedWithResponse(ctx, contract.ManagedRenderRequest{Graph: g, Stack: stack, Lock: lock})
	if err != nil {
		return contract.RenderedStack{}, errors.Join(deploy.ErrRenderer, err)
	}
	switch {
	case resp.JSON200 != nil:
		return *resp.JSON200, nil
	case resp.JSON422 != nil:
		return contract.RenderedStack{}, &deploy.RefusedError{Problems: resp.JSON422.Problems}
	case resp.JSON400 != nil:
		return contract.RenderedStack{}, &deploy.RefusedError{Problems: resp.JSON400.Problems}
	}
	return contract.RenderedStack{}, rendererError("managed render", resp.StatusCode(), resp.Body)
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

// Apply starts an apply of the stack.
func (t *Target) Apply(ctx context.Context, composeProject string, req deploytarget.ApplyRequest) (deploy.TargetOperation, error) {
	resp, err := t.c.ApplyStackWithResponse(ctx, composeProject, req)
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

// Remove starts a remove of the stack.
func (t *Target) Remove(ctx context.Context, composeProject string, deleteData bool) (deploy.TargetOperation, error) {
	resp, err := t.c.RemoveStackWithResponse(ctx, composeProject, deploytarget.RemoveRequest{DeleteData: deleteData})
	if err != nil {
		return deploy.TargetOperation{}, errors.Join(deploy.ErrTargetUnavailable, err)
	}
	switch {
	case resp.JSON202 != nil:
		return deploy.TargetOperation{Operation: *resp.JSON202, BootID: resp.HTTPResponse.Header.Get(bootHeader)}, nil
	case resp.JSON404 != nil:
		return deploy.TargetOperation{}, deploy.ErrNotFound
	case resp.JSON409 != nil:
		return deploy.TargetOperation{}, deploy.ErrBusy
	}
	return deploy.TargetOperation{}, targetError("remove", resp.StatusCode(), resp.Body)
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

// State reads the stack's containers.
func (t *Target) State(ctx context.Context, composeProject string) (deploytarget.StackState, error) {
	resp, err := t.c.GetStackStateWithResponse(ctx, composeProject)
	if err != nil {
		return deploytarget.StackState{}, errors.Join(deploy.ErrTargetUnavailable, err)
	}
	if resp.JSON200 == nil {
		return deploytarget.StackState{}, targetError("state", resp.StatusCode(), resp.Body)
	}
	return *resp.JSON200, nil
}

// Secrets lists the stack's secret files that exist.
func (t *Target) Secrets(ctx context.Context, composeProject string) ([]string, error) {
	resp, err := t.c.GetStackSecretsWithResponse(ctx, composeProject)
	if err != nil {
		return nil, errors.Join(deploy.ErrTargetUnavailable, err)
	}
	if resp.JSON200 == nil {
		return nil, targetError("secrets", resp.StatusCode(), resp.Body)
	}
	return resp.JSON200.Present, nil
}

// Images reads the stack's image locks.
func (t *Target) Images(ctx context.Context, composeProject string) (deploytarget.StackImages, error) {
	resp, err := t.c.GetStackImagesWithResponse(ctx, composeProject)
	if err != nil {
		return deploytarget.StackImages{}, errors.Join(deploy.ErrTargetUnavailable, err)
	}
	switch {
	case resp.JSON200 != nil:
		return *resp.JSON200, nil
	case resp.JSON409 != nil:
		return deploytarget.StackImages{}, deploy.ErrBusy
	case resp.JSON500 != nil:
		return deploytarget.StackImages{}, fmt.Errorf("%w: %.200s", deploy.ErrLockInvalid, resp.Body)
	}
	return deploytarget.StackImages{}, targetError("images", resp.StatusCode(), resp.Body)
}

// Resolve asks the deploy target for each reference's registry digest.
func (t *Target) Resolve(ctx context.Context, references []string) (deploytarget.ResolveResult, error) {
	resp, err := t.c.ResolveImagesWithResponse(ctx, deploytarget.ResolveRequest{References: references})
	if err != nil {
		return deploytarget.ResolveResult{}, errors.Join(deploy.ErrTargetUnavailable, err)
	}
	if resp.JSON200 != nil {
		return *resp.JSON200, nil
	}
	if resp.JSON400 != nil {
		// a reference the deploy target refuses cannot be resolved: image_unresolved per reference
		out := deploytarget.ResolveResult{}
		for _, p := range resp.JSON400.Problems {
			p.Code = "image_unresolved"
			out.Problems = append(out.Problems, p)
		}
		return out, nil
	}
	return deploytarget.ResolveResult{}, targetError("resolve", resp.StatusCode(), resp.Body)
}
