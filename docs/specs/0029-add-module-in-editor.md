# 0029. Add a module in the editor

**Date**: 2026-11-10
**Status**: In Progress

## Summary

You add a module by pressing Add on its entry in the catalog panel. The editor names the new instance after the module (`postgres`, then `postgres-2`), puts its node in a free spot, selects it and tells you it was added. It is one undo step and one check call, like every other edit. This is the missing first step of "design online, download, compose up": until now the editor could only change a graph that already held modules.

## Context

<!-- DECISION RECORD -->
Workspace: `web/` (the editor, spec 0010), a repo wide scope row (#6) but a change local to the editor. Mode: ENHANCEMENT.

The editor can open a graph, bind, rebind, unbind, undo, tidy and filter the catalog. It cannot place a module, so a graph can only come from a file or the sample. Scope row #6 needs a person to build a two module graph from nothing, take the checks' recommendation, and download a zip that runs.

Forces from the code and earlier specs:
- An instance is a key of `graph.modules` and becomes the Compose service name, so it must match the DNS label rule of spec 0002 and avoid its reserved words.
- Every edit already goes through one path: record an undo step, apply, send one `POST /v1/check` (spec 0010 AC-12, AC-15).
- Positions live only in the working copy. `withPositions` runs the full auto layout when any instance has none, which would move every node (AC-16 forbids that while editing).
- Spec 0028 says the checks recommend a provider and the editor never binds one on its own.
- The sample route is read only and copies on the first edit (0010 AC-18). With no graph open there is no working copy at all.

Scope row #6 also lists deleting and renaming. The engineer chose to design only adding here.

## Requirements

<!-- BUILD SPEC -->

**User stories**:
- As someone designing a stack, I want to add a module from the catalog so that I can build a graph without opening a file first.
- As a keyboard or screen reader user, I want adding to work without dragging and to tell me what happened.
- As someone who made a mistake, I want one undo to take the module back.

**Acceptance criteria**:
- **AC-1**: Each catalog entry has an Add button named "Add <module name>", reachable with Tab and pressed with Enter, Space or a click. Pressing it adds one instance of that module to the working graph. Dragging from the catalog is not part of this spec. Add is not shown while the working copy loads or when the catalog failed (the screen then shows Failure). It works while a check runs or after one failed (AC-6). The managed stack view has no Add.
- **AC-2**: The new instance is named after the module. When that key is already in `graph.modules` (a module or an external), the name is `<module>-N` with the lowest N of 2 or more that is free. When the result would be longer than 63 characters, the module part is cut so the whole name is 63 (the cut depends on the digits of N; a cut may leave a double hyphen, which is still valid). Catalog names already pass `ValidName` (the SDK manifest check, spec 0002 rules, reserved words included), and a `-N` suffix is never a reserved word, so no new name rule is needed. The new entry is `{ module: <module name> }`: no `uses`, no `config`.
- **AC-3**: The same module may be added any number of times. The editor has no per module limit and copies no catalog rule; the checks judge the rest.
- **AC-4**: The new node is placed in column 0 (x is 0). Its y is the lowest bottom edge of any node at x 0 (its y plus its drawn height, from the same height rule `flow.ts` uses) plus the gap between rows, or 0 when none is there, so it never overlaps a tall node. No existing position changes. The instance and its position are written in the same `set`, so the check reply's `withPositions` never sees an instance with no position and never runs the full layout. A later Tidy layout may move it.
- **AC-5**: Add is one history step, like connect and Tidy (it extends spec 0010 AC-15). The step holds the graph and positions from just before. Undo removes the instance and its position, then sends one check call. Redo brings back the same name and position, then sends one check call. A new add clears the redo steps. The edited flag is set (0010 AC-17). After an undo of an add the selection is cleared (0010 AC-8) and focus stays on the Undo button when it was pressed, or moves to the canvas region when the shortcut was used. Redo selects the node again and moves focus to it.
- **AC-6**: One `POST /v1/check` goes out per add, with the new instance in the graph. The rules of 0010 AC-12 and AC-14 apply: the add shows at once, a newer call aborts an older one, only the latest reply is used, and a failed check keeps the add and shows Failure with Retry. Adding while a check runs works.
- **AC-7**: The editor writes no binding when it adds. If the new instance has a required need with one provider in the graph, the checks recommend it (spec 0028) and the inspector shows Accept, exactly as for any left out need.
- **AC-8**: After an add, the new node is selected, keyboard focus moves to it, and the canvas centers on it at the current zoom (at least 1), without animation under `prefers-reduced-motion` (the same rules as 0010 AC-13). A polite status line says "Added <instance>" in the existing header status note. The store keeps it in an `announcement` field that undo, redo, open, Tidy and any other edit clear first, so an add repeated after an undo is announced again. The store sets a `centerRequest`; the canvas handles it after the next frame, using the stored position and the node's known size (the node is not drawn yet when Add is pressed). The problems panel uses the same path. The catalog filter keeps its text and Add keeps working for the next module.
- **AC-9**: With no graph open, the editor root shows the same `GraphScreen` as an open graph, as one element with the same key and place in the tree, so the first add does not remount the canvas or wipe the new selection and focus. It shows the catalog panel and an empty canvas with the "No graph open" message over it. Open file and the sample link stay in the toolbar. Its inputs: the graph `{ apiVersion: 'modular-platform/v1', kind: 'Graph', name: 'untitled', modules: {} }`, a placeholder check `{ ok: true, graph: <that graph>, bindings: [], problems: [] }`, and the file name `untitled.yaml`. Save as file is disabled until the first add. The first add makes the working copy from those values with the new instance and its position, then the add. History holds the empty graph, so one undo returns to an empty graph, not to the "No graph open" message. Nothing is saved to IndexedDB until a check reply for exactly that graph arrives (0010 AC-12).
- **AC-10**: While the graph has no modules, Download zip and Deploy are disabled with the reason "Add a module first", which comes before the other reasons of 0010 AC-14 (an empty graph checks as ok). Save as file stays enabled once a working copy exists (AC-9).
- **AC-11**: The catalog panel calls `onAdd(module)` from `Editing`. The editor root wires it to the store. The sample route wires it to its hold and guard flow: Add is held like any first edit (0010 AC-18), with the replace Dialog if the working graph is edited, a second Add while the Dialog is open dropped, then the copy is made with the module and its position (taken from the sample's layout), one undo returns to the unedited copy, and you land in the editor with the new node selected and the status and centering carried across the navigation.
- **AC-12**: The Add buttons pass the axe check in both themes at the same states as the rest of the catalog panel, and the catalog text still renders as plain text (0010 AC-20).

## Options considered

<!-- DECISION RECORD -->
### Option 1: Add button, auto name, place the new node only

A button on each catalog entry adds at once with an automatic name and a local placement. Naming and removing come later.

**Pros**:
- Works with keyboard, touch and the stacked layout below 1024px, with no second path.
- One step, one check call, one undo step.
- Existing nodes never move (AC-16).

**Cons**:
- The name is not yours to choose until rename exists (follow up).

### Option 2: Drag from the catalog onto the canvas, with a name dialog

Feels natural on a canvas and lets you choose the name first.

**Pros**:
- You choose where it lands and what it is called.

**Cons**:
- Needs a keyboard path anyway, so two paths to build and test.
- A dialog per add slows adding five modules in a row.
- Dragging a node is not allowed in 0010 (AC-16), so a drop position has no matching editor concept yet.

### Option 3: Add button, then full auto layout

Simplest placement: add and run the layout again.

**Pros**:
- The graph is always tidy.

**Cons**:
- Every add moves every node, which AC-16 of spec 0010 rules out.

## Decision

<!-- BUILD SPEC -->
**Chosen option**: Option 1: Add button, auto name, place the new node only

Adding is the existing `edit` path widened to carry positions: one history step, the instance and its position written together, selection, focus and centering, one check call; with no graph open it first makes an empty working copy.

Amends spec 0010: AC-15 lists add among the undoable edits, AC-16 gives the new node a position while existing ones stay, AC-18 holds the Add press on the sample like any first edit, and AC-14's Deploy rule gains the empty graph reason.

**Implementation skills**: `react-flow` (`existential-birds/beagle`, `.agents/skills/react-flow/`) · `react-state-management` (`wshobson/agents`, `.agents/skills/react-state-management/`) · `vercel-react-best-practices` (`vercel-labs/agent-skills`, `.agents/skills/vercel-react-best-practices/`)

## Rationale

<!-- DECISION RECORD -->
The editor already funnels every edit through one record, apply and check path, so add costs one new pure function and one store action, not a new mechanism. A button is the one path that also serves keyboard and the stacked layout, which the WCAG AA rule of the web conventions needs anyway. Choosing a name in a dialog adds a step to every add and duplicates what rename will do, so the automatic name wins; the module name is already a valid DNS label, so it is a safe default. Placing only the new node keeps AC-16 true, and column 0 matches where `layout()` puts any instance nobody uses, so a Tidy later changes little. Letting the checks recommend, not bind, keeps spec 0028 whole and keeps add a change to modules only.

Runner up for placement: the centre of the view. It lands where you look but can overlap nodes, which needs overlap handling for no gain.

## Feature design

<!-- BUILD SPEC -->

**Data model sketch**: no new entity and no API change. One new graph entry per add: `modules.<instance> = { module: <catalog name> }`. One new position per add: `positions.<instance> = { x, y }` in the working copy only, never in a graph file. The empty start graph is `{ apiVersion: 'modular-platform/v1', kind: 'Graph', name: 'untitled', modules: {} }`.

**Behaviour** (pure functions in `web/src/graph/`, no React, no I/O):
- `addModule(graph, module)` in `edit.ts` returns the new graph and the instance name (AC-2, AC-3).
- `placeNew(graph, catalog, positions)` in `layout.ts` returns the position for the new node, using the node height rule of `flow.ts` (AC-4).
- `edit(change)` in the store accepts a change that returns `{ graph, positions }` as well as a graph, so add reuses the one record, apply and check path (AC-5, AC-6). A thin `addModule(moduleName)` builds the empty working copy when `working` is null, calls `edit`, then `focusModule`, sets `announcement` and `centerRequest` (AC-8, AC-9). `announcement` is cleared by undo, redo, open, Tidy and other edits.
- `copySample` takes the same `{ graph, positions }` change, its no change test compares both, and `handoff` carries the new instance as the selection (AC-11).
- `Editing` gains `onAdd(module)`; the catalog panel calls it, the editor root wires it to the store, the sample route to its guard flow (AC-1, AC-11).
- `downloadBlocked` gets the module count and the empty graph reason first; Deploy uses it too (AC-10).

**State transitions**: none new. History is the existing past and future lists (0010 AC-15).

**API surface**: none. The renderer contract, the catalog and the graph schema stay as they are.

**Value sourcing**:
| Action | Value produced / displayed | Source |
|---|---|---|
| Add | instance name | the catalog entry's `name`, plus the keys of `graph.modules` for the suffix (AC-2) |
| Add | the 63 character cut | the DNS label rule in spec 0002 |
| Add | position | `positions` of the working copy, node heights from `flow.ts`, the row gap from `layout.ts` (AC-4) |
| Add | "Added <instance>" lifetime | the store's `announcement`, cleared as in AC-8 |
| Add | where the canvas centers | the store's `centerRequest`, handled by the canvas after the next frame (AC-8) |
| Add on the sample | positions of the copy | `layout()` of the sample, as the sample route already computes (AC-11) |
| Empty screen | check, file name | the placeholder check and `untitled.yaml` of AC-9 |
| Add | `module` field | the catalog entry's `name` |
| Add with no graph open | graph header, name, file name | constants in this spec (AC-9) |
| Add | focus and selection | the new instance name, through `focusModule` (AC-8) |
| Add | "Added <instance>" | the new instance name; the store holds it until the next edit (AC-8) |
| Add | centre point of the canvas | the node's stored position and the viewport, as for a clicked problem (0010 AC-13) |
| Undo or redo of an add | graph and position | the history step (AC-5) |
| Empty graph download and deploy gate | "Add a module first" | `Object.keys(graph.modules).length === 0`, passed to `downloadBlocked` (AC-10) |

**Key invariants**:
- An add never changes another instance, a binding or an existing position.
- The new key is never already in `graph.modules` (checked with `Object.hasOwn`, since names come from a file or a catalog).
- Graph files still hold no positions.

**Security model**: none new. No new input from outside: the module name comes from the loaded catalog and renders as plain text (0010 AC-20).

**Configuration required**: none.

**Critical test scenarios**:
- Happy path: with nothing open, add `whoami` and `postgres`, accept the recommendation on whoami, download the zip, verifies **AC-1**, **AC-6**, **AC-7**, **AC-9**, **AC-10**
- Naming: add `postgres` three times; the names are `postgres`, `postgres-2`, `postgres-3`; with `postgres-2` present and `postgres` removed, the next is `postgres`; a 63 character module name gets a cut name, verifies **AC-2**, **AC-3**
- Placement: positions of existing nodes are unchanged after an add; the new node sits one row below the lowest node at x 0, verifies **AC-4**
- Undo and redo: add, undo (instance and position gone, one check call), redo (same name and position), add again after undo (redo cleared), verifies **AC-5**
- Latest wins: add twice with the first reply delayed past the second; only the second reply applies; add while the first check runs works, verifies **AC-6**
- Failure case: the check route answers 503; the add stays, problems show "not checked", Retry recovers, verifies **AC-6**
- No binding written: add `whoami` while `postgres` exists; `uses` of the new instance is empty and the inspector shows "Recommended: postgres", verifies **AC-7**
- Keyboard only: Tab to Add, press Enter; focus lands on the new node and the status says "Added whoami", verifies **AC-1**, **AC-8**
- Sample: add on the sample route with an edited working graph; the Dialog shows; Replace makes the copy with the module, one undo returns to the unedited copy, verifies **AC-11**
- Empty download: with only an empty graph, Download zip and Deploy are disabled with "Add a module first", verifies **AC-10**
- Announcement: add, undo, add again; the status reads "Added postgres" both times and is empty after the undo, verifies **AC-8**
- First add keeps focus: on the empty screen, add a module; the node is selected and focused (no remount), verifies **AC-8**, **AC-9**
- Tall node: add below a node with four ports; the nodes do not overlap, verifies **AC-4**
- Accessibility: axe on the catalog panel with Add buttons, both themes, verifies **AC-12**

## Build plan

<!-- BUILD SPEC -->
Approach: Tracer Bullet. The first task runs the whole thread (button to check call) on a graph that is already open; later tasks thicken the start, the sample and the checks.

1. [x] `addModule` in `graph/edit.ts` and `placeNew` in `graph/layout.ts` with unit tests for naming, the 63 character cut, own key safety and placement beside a tall node, satisfies **AC-2**, **AC-3**, **AC-4**
2. [x] Widen `edit` to `{ graph, positions }` and add the store's `addModule` on an open graph: one history step, graph and position in one `set`, selection, `announcement`, `centerRequest`, one check call; unit tests for undo, redo (focus rules), latest wins and a failed check, satisfies **AC-5**, **AC-6**, **AC-7**, **AC-8**
3. [x] `onAdd` in `Editing`, the Add button on each `CatalogPanel` entry with its availability rules, the status line, the canvas handling `centerRequest` (the problems panel moves to it too), the Add buttons in the axe check, satisfies **AC-1**, **AC-8**, **AC-12**
4. [x] No graph open: one `GraphScreen` element for both states, the placeholder check and file name, the first add makes the working copy, Download zip and Deploy gated with "Add a module first", satisfies **AC-9**, **AC-10**
5. [x] Sample route: `copySample` takes `{ graph, positions }`, `onAdd` goes through the guard and replace Dialog, the handoff carries the selection, satisfies **AC-11**
6. [x] Browser check and smoke: add two modules from nothing, accept, download the zip, run it in `task smoke` as the scope row's "design online, download, compose up" path, satisfies **AC-1**, **AC-6**, **AC-7**, **AC-9**, **AC-10**

## Consequences

<!-- BUILD SPEC -->
**Positive**:
- A graph can be built from nothing in the editor, the missing start of scope row #6.
- No contract, catalog or renderer change, so `task generate` stays clean.
- Add reuses the one edit path, so history, latest wins, failure and the edited flag need no new rules.

**Negative / tradeoffs**:
- You cannot choose or change an instance name until rename lands; `postgres-2` stays.
- Column 0 can fill up with unbound nodes in a tall stack until you press Tidy layout.
- The "No graph open" screen is no longer a bare empty state; it renders the editor on an empty graph and loads the catalog earlier.
- Spec 0010 now needs AC-15, AC-16 and AC-18 read together with this spec.

**Neutral**:
- An empty graph can be saved as a file but not downloaded as a zip.
- Remove and rename share the history and selection rules written here.

## Follow-up

- [ ] Remove a module (and what happens to bindings that point at it) and rename an instance (rewriting bindings): the rest of scope row #6, each its own design
- [ ] Drag from the catalog onto the canvas, if testing shows people want it (needs a keyboard path anyway)
- [ ] Spec 0010 AC-15, AC-16 and AC-18 name only the older edits; `/sync` should flag them against this spec
