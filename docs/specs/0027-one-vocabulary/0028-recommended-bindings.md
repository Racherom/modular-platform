# 0028. Recommended bindings

**Parent**: [0027. One vocabulary](index.md)

## Summary

Today, when a module needs a capability and exactly one module in the graph provides it, the checks bind them on their own and mark the binding `auto`. From now on they only recommend that provider. You accept it in the editor (one use at a time, or all at once), which writes an ordinary binding into the graph. Until you do, the graph does not render or apply. The graph file is then the whole truth about how a stack is wired.

## Rules (`libs/check`)

For each use `s` of instance `i` that the graph leaves out (no key under `modules.<i>.uses`), with `matches` the provides that serve the slot's capability inside its version range (the candidate search of 0012, unchanged):

| Slot | Matches | Result |
|---|---|---|
| required | 0, some outside the range | `version_mismatch` (error), unchanged |
| required | 0 | `unmet_need` (error), unchanged |
| required | 1 | **`binding_recommended`** (error), `candidates: [<that provide>]`, message `Instance <i> needs <capability> for <s>; <candidate> provides it. Accept the recommendation or pick another provider.` |
| required | 2 or more | `ambiguous_provider` (error) with every candidate, unchanged; no recommendation |
| optional | 1 or more | `optional_unbound` (warning) with every candidate, unchanged |
| optional | 0 | nothing, unchanged |

- The candidate text is today's: the instance name when it has one provide serving the capability, else `<instance>.<provide>`.
- The checks never write a binding for a left out use: `CheckResult.graph` keeps the use left out, and `CheckResult.bindings` has no entry for it.
- A use set to `null` stays "explicitly unbound" (`unbound_required` when required), unchanged.
- `binding_recommended` is an error, so `ok` is false and the existing rule "render and apply only an `ok` result" refuses it: `POST /v1/render`, `/v1/render/managed`, `/api/v1/render`, and `review`/`apply` on `/api/v1/stacks/{name}`.
- `Binding.auto` is removed from `renderer.yaml`; every binding in `CheckResult.bindings` comes from the graph.
- Defaults that are not bindings stay filled by the checks in the resolved graph: provisioner named values (`check.DefaultName`) and grant sets (0023). They are rules with one answer, not choices.
- No optional use is ever bound without a click: `optional_unbound` stays a warning, as today.
- **`v0alpha1` conversion adds no bindings.** A converted graph with a gap carries both the `converted_from_v0alpha1` warning and `binding_recommended`, so an old graph or zip opened in the editor shows its recommendations and renders once they are accepted.
- **Digests.** Removing `auto` changes every digest value. A stack reviewed before the upgrade gets `graph_changed` at apply and is reviewed again (AC-6's note).

## Editor (`web/`, both editors)

- **Inspector, per use**: when the latest check result has `binding_recommended` (or `optional_unbound` with exactly one candidate) at this use's path, the binding field shows "Recommended: `<candidate>`" and an **Accept** button. Accept calls `acceptRecommendation(graph, consumer, slot, candidate)`, which is `bind(...)` with that candidate: the same edit as picking the provider from the list. The option "Let the planner choose" and the "Bound by the planner" note are removed; the list keeps "Not bound" and the candidates.
- **Toolbar**: "Accept all recommendations (N)" shows while N > 0, where N counts `binding_recommended` problems only (optional uses are not accepted in bulk). It runs one `useEditor.edit(change)` (one history entry, so one undo reverts it) whose `change` is `acceptAllRecommendations(graph, problems)`. Inside it, each recommendation is bound only if its consumer still exists, the use is still left out, and its candidate instance still exists in that graph; any that fail are skipped, and a notice says how many were skipped. The edit starts a new check as every edit does.
- **Download and apply** stay disabled while N > 0, with the reason "Accept or change N recommended bindings first" shown beside the disabled button and tied to it with `aria-describedby`, next to the existing rule for other errors.
- A stale check result (`ctx.checkCurrent` false) shows no Accept, so you never accept against an old graph.
- **Focus**: after Accept in the inspector, focus moves to that use's binding field (which now shows the provider); after Accept all, it stays on the toolbar, where the button is gone, so focus moves to the toolbar's first control. The Accept button's accessible name is "Accept <candidate> for <slot>".
- **Problem list**: a `binding_recommended` entry reads like any problem and, as today, selecting it opens the inspector at that use; it has no Accept of its own.
- The editor shows a recommendation the same way for any graph it opens: a sample, a shared graph, a zip from before #41 (#42).

## Shipped graphs

Every graph the repo ships to be rendered names each binding explicitly (AC-11 lists them). The fixtures that exist to show a gap stay as they are, and `required-one.yaml` drives the `binding_recommended` case. The `v0alpha1` fixtures stay as they are; their conversion runs the same checks, so a converted graph with a gap shows `binding_recommended`. The managed platform renders its own graph at the one container start (0016) and in `catalog/cmd/render-platform` with no one to accept, so its graph must have no gap: a test asserts `check.Graph` on `deploy/managed/graph.yaml` is `ok`, and `render-platform` fails on errors only, never on warnings.

## Tests

- `libs/check`: table cases for each row above; that `CheckResult.graph` and `bindings` hold no entry for a recommended use; the `v0alpha1` sample with a gap converts to `binding_recommended`.
- `services/renderer` and `services/api` (`httptest`): `/v1/render` and `/api/v1/render` answer the error status with the `CheckResult` for a graph with an open recommendation.
- Editor (vitest and Testing Library): Accept writes the binding and moves focus to the binding field; Accept all writes N bindings in one undo step and skips one whose candidate was removed; download is disabled while N > 0 with its reason announced; no Accept on a stale result; axe stays clean.
- Render golden files: unchanged apart from renamed words, now that the inputs bind explicitly.

## Rationale (short)

You chose recommendations the admin accepts over bindings the checks write. Before, two people with the same graph and different catalogs could get different wiring with no line in the graph to show it; now the graph holds every binding, so the zip, the stack editor and a review all read the same thing. The cost is a click per gap (one, with Accept all). Recommending only with exactly one match keeps the checks from picking between equals by an arbitrary rule.
