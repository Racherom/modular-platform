package api

import (
	"github.com/racherom/modular-platform/services/api/internal/deploy"
)

// operation maps a history row onto the contract; it never carries a placed value, only paths.
func operation(op deploy.Operation) Operation {
	out := Operation{
		Id: op.ID.String(), Action: OperationAction(op.Action), Status: OperationStatus(op.Status),
		Step: OperationStep(op.Step), StartedAt: op.StartedAt, StartedBy: op.StartedBy.Email,
		Error: op.Error, DeleteData: op.DeleteData, PlacedSupplied: op.PlacedSupplied, Digest: op.Digest,
	}
	if out.PlacedSupplied == nil {
		out.PlacedSupplied = []string{}
	}
	if op.FinishedAt != nil {
		out.FinishedAt = *op.FinishedAt
	}
	if op.RollbackOf != nil {
		out.RollbackOf = op.RollbackOf.String()
	}
	return out
}

func summary(d deploy.Stack) StackSummary {
	out := StackSummary{Name: d.Name, State: StackState(d.State())}
	if d.Latest != nil {
		out.LatestOperation = operation(*d.Latest)
	}
	return out
}

func moduleStates(in []deploy.ModuleState) []ModuleState {
	out := make([]ModuleState, 0, len(in))
	for _, m := range in {
		out = append(out, ModuleState{
			Instance: m.Instance, Module: m.Module, Image: m.Image, State: ModuleStateState(m.State), Health: m.Health,
			ExitCode: m.ExitCode, LastExitCode: m.LastExitCode, RestartCount: m.RestartCount, Since: m.Since,
		})
	}
	return out
}

func view(v deploy.View) Stack {
	s := summary(v.Stack)
	out := Stack{
		Name: s.Name, State: s.State, LatestOperation: s.LatestOperation, ComposeProject: v.ComposeProject, CreatedAt: v.CreatedAt,
		StateError: v.StateError,
	}
	if v.Graph != nil {
		out.Graph = *v.Graph
	}
	if v.LatestApplyGraph != nil {
		out.LatestApplyGraph = *v.LatestApplyGraph
	}
	if v.RolledBackFrom != nil {
		out.RolledBackFrom = operation(*v.RolledBackFrom)
	}
	if v.StateError == "" {
		out.Modules = moduleStates(v.Modules)
	}
	return out
}
