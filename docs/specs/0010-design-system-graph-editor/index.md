# 0010. Design system and graph editor foundation

**Date**: 2026-11-03
**Status**: In Progress

## Summary

The editor gets a calm, technical look that every renderer screen builds on: design tokens (named colors, sizes and spacing) in one CSS file, light and dark themes with a toggle, system fonts, and a small set of accessible components on React Aria Components, all written down in `web/design.md`. The graph canvas becomes editable for the first time: you connect a module's need to another module's offer by dragging, remove or change a binding, and do the same from a keyboard friendly inspector panel; the checks check every edit. Placing, deleting and renaming modules stay in #6, and the read only stack stays in #16.

## Requirements

**User stories**:
- As a self hoster, I want to wire one module's need to another module's offer on the canvas so that I can build a working stack without editing YAML by hand.
- As a keyboard or screen reader user, I want every canvas edit available from the inspector so that I can build a graph without a mouse.
- As a platform team member, I want problems shown on the exact port that has them so that I can fix a broken graph quickly.
- As a contributor building later screens (#16, #6), I want documented tokens and components so that new screens look the same without inventing styles.

**Acceptance criteria** (the contract):

Design system
- **AC-1**: `web/design.md` documents every token (color, type, spacing, radius, shadow, layer, motion, layout) with its CSS custom property name and its light and dark values, every component with its variants, states and when to use it, and the canvas visual grammar (node anatomy, ports, edges, selection, problem marks). All tokens are defined only in `web/src/styles/tokens.css`; a unit test fails when any other CSS file under `web/src/`, or any `style` prop in a `.tsx` file under `web/src/` (tests excluded), contains a hex, `rgb(`, `rgba(`, `hsl(` or `hsla(` color literal.
- **AC-2**: The theme follows the OS setting by default. A header control offers System, Light and Dark; the choice is stored in `localStorage` key `mp-theme` and applied before first paint (no flash of the other theme on reload). A missing or unknown stored value means System; if storage throws, the toggle still works for the session. React Flow follows the same resolved theme.
- **AC-3**: In light and in dark theme, at a 1280×800 viewport, axe (tags `wcag2a`, `wcag2aa`, `wcag21a`, `wcag21aa`, plus `wcag22aa` if the installed axe core accepts it) reports zero violations on: the editor with the sample graph, the editor with a module selected, the editor during a connect drag (pointer held down over the canvas, valid targets highlighted), the editor with problems shown, and the `/design` route. The only allowed exclusions are rules hit by React Flow's own markup that its props (`ariaLabelConfig`, `ariaRole` and similar) cannot fix; each is listed in `design.md` with the rule id and the reason. Every interactive element, including canvas nodes and edges, shows a visible focus indicator with at least 3:1 contrast against its background.
- **AC-4**: The component set exists under `web/src/components/ui/`, each with a `.module.css` file, interactive ones built on React Aria Components: Button (primary, secondary, ghost, danger; sizes md and sm), IconButton (accessible name required, shows a Tooltip), Select, SearchField, Tooltip, Badge, StatusBadge (tones neutral, info, success, warning, danger; dot plus text label, never color alone), Panel, Toolbar (arrow keys move between its controls), EmptyState, Failure (restyled, same props as today), ThemeToggle and Dialog (focus trapped, Escape closes, focus returns to the opener).
- **AC-5**: `/design` renders every token and every component in every listed state, with a control to switch the theme there. It exists only in dev builds: the route is part of the committed `routeTree.gen.ts`, but in a production build `/design` shows the not found screen, and the built bundle contains none of the showcase code. The browser check, which builds the app for its preview server, searches `dist/` for a marker string from the showcase.

Layout
- **AC-6**: From 1024px wide, the screen shows a header (app name, file name with the edited marker, toolbar, theme toggle), the catalog panel on the left, the canvas in the middle, the inspector on the right and the problems panel below the canvas; the problems panel can be collapsed and expanded. Below 1024px the panels stack under the canvas, the graph still shows and selection still works, and there is no horizontal page scroll down to 360px. Editing below 1024px is allowed but not tested.

Canvas
- **AC-7**: The canvas draws one node per instance (instance name, module name, provides ports on the left, uses ports on the right), required and optional needs distinct without relying on color (filled versus hollow dashed handle, plus the word "optional" in the inspector), bindings as smooth step edges with the arrow at the provider, a dot grid background and zoom in, zoom out and fit controls. With a generated graph of 50 modules and 100 bindings, served as a production build by `vite preview` with the renderer mocked, a selection and a connect each update the screen within 100 ms (median of 5 runs, the check call not counted). A Vitest render count test shows that a connect renders again only the nodes whose data changed. Nodes cannot be dragged.
- **AC-8**: Clicking a node or an edge (or focusing it and pressing Enter) selects it; only one item is selected at a time. The inspector shows it; the selected module's edges and ports, or the selected binding's two ports, are highlighted; unrelated edges and node borders dim while all text keeps AA contrast. Escape or a click on empty canvas clears the selection. When a selected binding stops existing (unbind, undo, redo or a check reply), its consumer module becomes the selection and keyboard focus moves to that node.
- **AC-9**: Dragging from a uses port highlights every valid target and dims the other ports. A valid target is a provides port with the identical capability name on another instance whose module is in the catalog. Dropping on a valid target sets `modules.<consumer>.uses.<capability>` to the provider instance, replacing any previous provider. Dragging the provider end of an existing edge to another valid target rebinds it; the consumer end cannot be dragged. A drop anywhere else, for a new connection or a reconnect, changes nothing and the edge stays. Dragging from a provides port starts nothing. A drop that closes a cycle is allowed; the checks report it.
- **AC-10**: With a binding selected and focus on the canvas, Delete or Backspace, or the inspector's Remove binding button, sets that need to `null`; the consumer module is then selected (AC-8). Delete and Backspace typed in a text field or select never unbind. Deleting never removes a module. A required need set to `null` shows the checks' `unbound_required` error on its port.
- **AC-11**: The inspector of a selected module lists every uses need with its capability, "required" or "optional", and a "Bound to" select whose options are: each candidate instance (sorted by name), "Not bound" (`null`), and "Choose a provider" (disabled, shown while the use is left out). Choosing an option makes the same edit as dragging. When the checks recommend a provider for a left out use (spec 0028), the field shows "Recommended: <provider>" with an Accept button, which writes the same binding as picking that provider. A current binding to an instance that is not a candidate (a broken file) shows as "<name> (invalid)". Nodes and edges are reachable with Tab, and every edit in #5 can be done from the keyboard alone.
- **AC-12**: After every edit the canvas shows the edit at once and the problems show "checking"; the editor sends one `POST /v1/check` with the edited graph. A new check call aborts the one before it. Only the reply for the latest edit is used: its `graph` (with the defaults the checks fill in, as spec 0002 writes them back) and the resolved graph replace the working graph and check result, without adding a history step, and the working copy (graph, check result, positions, file name, edited flag) is saved to IndexedDB. An older reply that arrives later is dropped. The working copy is saved only with a check result for exactly its graph, never while the resolved graph is checking or failed.
- **AC-13**: A problem whose path is `modules.<i>.uses.<C>` marks port `C` of node `i`; any other path starting `modules.<i>` marks node `i`; any other problem shows only in the panel. A port problem whose port is not drawn (unknown module, broken file) marks the node instead. Marks use an icon plus color by severity (error, warning); when one port or node has several problems, the error mark wins and its accessible name lists every message. The inspector lists the selected module's problems. Clicking a problem in the problems panel selects its module, centers the canvas on it at the current zoom (at least 1), without animation under `prefers-reduced-motion`, and moves focus to the node.
- **AC-14**: When the check call fails (network error, 429, 5xx), the edit stays, the problems show "not checked", and a Failure with Retry appears (429 says the renderer is busy, try again in a minute). A 400 also counts as failed and shows the renderer's message, without Retry (undo is the way back). Download zip is disabled while the resolved graph is checking ("Checking the graph"), failed ("Not checked, retry first") or not `ok` ("Fix N errors to download", counting only `severity: error`); warnings never block. Deploy in managed mode (#16) uses the same rule.
- **AC-15**: Undo (Ctrl+Z, Cmd+Z on macOS) and redo (Ctrl+Shift+Z or Ctrl+Y, Cmd+Shift+Z on macOS), and toolbar buttons for both, step through connect, rebind, unbind, Accept and Tidy layout, one step each. History records only these user edits; a check reply never adds a step. Each step stores the working graph and positions as they were just before the edit (already filled in by the checks). Undo and redo restore that graph and those positions, then send a new check call (no check result is stored in history). Undo and redo work while a check is running; a new edit clears the redo steps. History is kept in memory and cleared when a graph is opened. The shortcuts do nothing while focus is in a text field or select.
- **AC-16**: Node positions stay where they are while you edit (nodes cannot be dragged in #5; positions are stored only so that a new binding does not move nodes). The auto layout runs for the whole graph when a graph is opened, when any instance has no stored position, and when you press Tidy layout. Tidy layout changes positions only and sends no check call. Positions live in the working copy only; Save as file writes exactly `stringifyGraph(graph)` and never a position.
- **AC-17**: An "edited" marker shows beside the file name after any edit (undo included) until Save as file or opening a graph. The flag is kept in the working copy, so it survives a reload. Leaving the page shows no prompt. When saving the working copy to IndexedDB fails, a quiet note says changes are not kept after reload in this browser.
- **AC-18**: The sample route shows the sample in the same editor, with a hint that changes make a copy; undo, redo and Tidy layout are disabled there. The first edit there (drag or inspector) is held before anything changes on screen. If the current working graph carries the edited flag, a Dialog first offers Save as file (saves the working graph, then replaces), Replace and Cancel; Cancel drops the edit, the inspector select reverts, and nothing changes. Otherwise, or on Replace, the editor makes the copy: file name `<graph name>.yaml`, the sample's current positions, history holding the unedited copy (one undo returns to it), the edit applied, the edited flag set, a check call started, and it navigates to the editor root. The sample itself never changes. Open file asks the same way, when the button is pressed and before any file is read.
- **AC-19**: The catalog panel lists every entry with name, description, and its provides and uses capabilities, with a filter that matches name, description and capability names, ignoring case, and an empty state when nothing matches. Each need in the inspector has a Find providers action that sets the filter to that capability, scrolls the catalog panel into view (also when it is stacked below 1024px) and moves focus to the filter. The filter text lives in the editor store.
- **AC-20**: An instance whose module is not in the catalog shows as a node with a "not in catalog" badge and no ports; it is never a valid drop target and never a candidate. Catalog descriptions and every name from a graph file render as plain text.

## Decision

**Chosen option**: CSS Modules over one token file, React Aria Components for interactive parts, and a canvas controlled from the graph (Option A1 with B1 in [rationale.md](rationale.md)).

Tokens in `web/src/styles/tokens.css`, component styles in CSS Modules, interactive components on React Aria Components, Lucide icons, system fonts; the graph in the Zustand store is the single source of truth, every edit is a pure function on the graph, React Flow only draws and reports gestures, and the checks check every edit.

**Implementation skills**: `react-flow` (`existential-birds/beagle`, `.claude/skills/react-flow/`) · `react-state-management` (`wshobson/agents`, `.claude/skills/react-state-management/`) · `design-system-patterns` (`wshobson/agents`, `.claude/skills/design-system-patterns/`) · `tanstack-query` · `tanstack-router` (both `tanstack-skills/tanstack-skills`, `.claude/skills/<name>/`) · `vite` (`antfu/skills`, `.claude/skills/vite/`) · `vercel-react-best-practices` (`vercel-labs/agent-skills`, `.claude/skills/vercel-react-best-practices/`, skip its Next.js advice) · `playwright-best-practices` (`currents-dev/playwright-best-practices-skill`, `.claude/skills/playwright-best-practices/`) · `ultracite` (`haydenbleasel/ultracite`, `.claude/skills/ultracite/`, Biome rules only) · `docs` (`minio/skills`, `.claude/skills/docs/`, for `design.md`)

## Rationale

Reasoning and options: see [rationale.md](rationale.md).

## Feature design

### Design system

**Direction**: calm technical tool. Neutral grays, one blue accent, crisp 1px lines, little decoration; the graph and its problems carry the color. Comfortable density. System fonts. Light and dark are equal citizens.

**Files**:
| Path | Owns |
|---|---|
| `web/src/styles/tokens.css` | Every token, light values on `:root`, dark values under `[data-theme="dark"]` and under `@media (prefers-color-scheme: dark)` for `:root:not([data-theme="light"])` |
| `web/src/styles/base.css` | Reset, `body`, links, focus ring, reduced motion |
| `web/src/styles/reactflow.css` | Maps React Flow's `--xy-*` variables to tokens; imported after `@xyflow/react/dist/style.css` |
| `web/src/components/ui/<Name>.tsx` + `<Name>.module.css` | Design system components (AC-4) |
| `web/src/components/` | Screen parts: `AppShell`, `GraphScreen`, `GraphView`, `ModuleNode`, `Inspector`, `CatalogPanel`, `ProblemsPanel` |
| `web/src/lib/theme.ts` | Reads, validates and stores `mp-theme`, resolves System, sets `data-theme` on `<html>` |
| `web/public/theme.js` | A classic (not module) script in `<head>`, before the app bundle. Inside try/catch it reads `mp-theme` and sets `data-theme` on `<html>` only when the value is exactly `light` or `dark`; anything else sets nothing and `tokens.css` follows the OS, so System logic is not duplicated. `index.html` writes its tag as `<script src="{{env `MP_BASE_PATH`}}/theme.js"></script>` (the placement plugin's dev branch already fills the template); `web/Caddyfile` serves `/theme.js` with `Cache-Control: no-cache` |
| `web/src/routes/design.tsx` | The showcase route (AC-5), committed in `routeTree.gen.ts`. Its `beforeLoad` throws `notFound()` unless `import.meta.env.DEV`; its component loads `web/src/design/Showcase.tsx` with `lazy(() => import(...))` inside an `import.meta.env.DEV` branch, so the production build drops the chunk |
| `web/design.md` | The design reference (AC-1) |
| `web/src/app.css` | Removed once its rules move into the files above |

**Color tokens** (starting values; design.md records the final ones after the axe check):
| Token | Light | Dark | Use |
|---|---|---|---|
| `--color-bg` | `#ffffff` | `#14161a` | Page |
| `--color-surface` | `#f6f7f9` | `#1b1e23` | Panels, header |
| `--color-surface-raised` | `#ffffff` | `#22262c` | Nodes, dialogs, popovers |
| `--color-border` | `#d9dde3` | `#343a43` | Dividers, panel edges |
| `--color-border-strong` | `#8a94a3` | `#6b7480` | Input and node borders (3:1 non text contrast) |
| `--color-text` | `#1b1f24` | `#e6e9ee` | Body text |
| `--color-text-muted` | `#5b6472` | `#a0a8b5` | Secondary text |
| `--color-accent` | `#2557d6` | `#7ea2ff` | Primary actions, selection, links |
| `--color-accent-hover` | `#1d47b3` | `#9bb8ff` | Hover on accent |
| `--color-on-accent` | `#ffffff` | `#0b1530` | Text on accent |
| `--color-focus` | `#2557d6` | `#7ea2ff` | Focus ring |
| `--color-danger` / `--color-danger-surface` | `#b42318` / `#fef3f2` | `#ff8a80` / `#3a1714` | Errors |
| `--color-warning` / `--color-warning-surface` | `#9a6700` / `#fff8e6` | `#f5c451` / `#33270a` | Warnings |
| `--color-success` / `--color-success-surface` | `#1a7f37` / `#edfaf0` | `#4ac26b` / `#0f2e19` | Success (StatusBadge) |
| `--color-canvas` / `--color-canvas-dot` | `#f6f7f9` / `#c9ced6` | `#14161a` / `#2c3139` | Canvas background and grid |
| `--color-edge` / `--color-edge-dimmed` | `#6b7480` / `#808a99` | `#8b94a1` / `#5f6774` | Bindings; dimmed edges still keep 3:1 against the canvas |

**Other tokens**:
| Group | Tokens |
|---|---|
| Fonts | `--font-sans: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif`; `--font-mono: ui-monospace, "SF Mono", "Cascadia Mono", Menlo, Consolas, monospace` (instance, module and capability names) |
| Type scale | `--text-xs` 12px, `--text-sm` 14px, `--text-md` 16px (body), `--text-lg` 18px, `--text-xl` 22px, `--text-2xl` 28px; line height 1.5 body, 1.25 headings; weights 400 and 600 |
| Spacing | 4px base: `--space-1` 4px, `--space-2` 8px, `--space-3` 12px, `--space-4` 16px, `--space-5` 20px, `--space-6` 24px, `--space-8` 32px, `--space-10` 40px, `--space-12` 48px |
| Radius | `--radius-sm` 4px, `--radius-md` 8px, `--radius-lg` 12px, `--radius-full` 9999px |
| Controls | `--control-md` 40px, `--control-sm` 32px |
| Shadow | `--shadow-1` (nodes, popovers), `--shadow-2` (dialogs); dark theme leans on borders, shadows stay faint |
| Layers | `--layer-panel` 10, `--layer-popover` 100, `--layer-dialog` 200 |
| Motion | `--duration-fast` 120ms, `--duration-base` 200ms, `--ease-standard`; all durations 0 under `prefers-reduced-motion: reduce` |
| Layout | `--header-height` 56px, `--catalog-width` 280px, `--inspector-width` 320px, `--problems-height` 200px, breakpoint 1024px (media queries use the literal, CSS variables cannot drive them) |
| Opacity | `--opacity-disabled` 0.5 (disabled controls only, never for dimming text) |

**Canvas visual grammar**: node width 240px, header 56px, port row 32px, handle 12px. The node geometry constants live in `web/src/graph/flow.ts` (React Flow needs handle positions before measuring) and `ModuleNode` writes them as CSS custom properties on its root, so CSS and TS share one source. Selected node: 2px accent border. Edge strokes: 1.5px normal, 2px accent when selected or related to the selection, 1px `--color-edge-dimmed` when dimmed (the width carries the dimming, not a faint color). Required need: filled handle; optional need: hollow dashed handle. Problem marks: Lucide `CircleAlert` in danger color (error), `TriangleAlert` in warning color (warning), with the problem message as the accessible name. Valid drop targets during a drag: accent ring; invalid ports: `--color-edge-dimmed` handle.

### Graph editing

**Data model sketch** (client state; no server or database change):
| Entity | Where | Fields | Notes |
|---|---|---|---|
| WorkingGraph | IndexedDB `modular-platform` / store `working` / key `graph` (exists, DB version stays 1) | `fileName: string` (req), `graph: Graph` (req, `v0alpha1`), `check: CheckResult` (req, last check result for exactly this graph), `positions?: Record<instance, {x, y}>` (new, optional), `edited?: boolean` (new, optional, missing means false) | An entry where any instance lacks a position gets the auto layout for the whole graph; position keys for unknown instances are ignored. Saved only with a check result for exactly its graph |
| Editor store | Zustand, memory | `working: WorkingGraph \| null \| undefined` (exists), `selection: null \| {kind: 'module', instance} \| {kind: 'binding', consumer, capability}`, `history: {past: Snapshot[], future: Snapshot[]}`, `planState: 'current' \| 'checking' \| 'failed'`, `planSeq: number`, `planError: RendererError \| null`, `dragFrom: {instance, capability} \| null` (drives the drop highlight), `catalogFilter: string`, `storageFailed: boolean` | `Snapshot = {graph, positions}`, taken just before a user edit; history cleared on open; the edited flag lives in `working.edited` |
| Theme preference | `localStorage` `mp-theme` | `'system' \| 'light' \| 'dark'` | Anything else reads as `system` |

Relations: a binding is identified by `consumer.capability` (one per need, as in spec 0002); selection and positions name modules and bindings by instance name only.

**Edit functions** (pure, in `web/src/graph/edit.ts`, no React, no I/O; each returns a new Graph):
| Function | Effect on `graph.modules[consumer].uses` |
|---|---|
| `bind(graph, consumer, capability, provider)` | `[capability] = provider` (connect and rebind) |
| `unbind(graph, consumer, capability)` | `[capability] = null` |
| `acceptRecommendation(graph, consumer, slot, candidate)` | the same edit as `bind` with that candidate (spec 0028) |
| `candidates(graph, catalog, consumer, capability)` | returns other instances whose known catalog entry provides `capability`, sorted by name (the candidate rule of spec 0002) |
| `isValidDrop(graph, catalog, source, target)` | true when the target handle is `provides:<C>` for the source handle's `uses:<C>`, on another instance with a known module |

**State transitions** (`planState`):
- open a graph: `current` (the open already checked)
- any graph edit (including the sample copy), undo, redo or Retry: increment `planSeq`, abort the previous call (`AbortController`), go to `checking`, send `POST /v1/check`; Tidy layout does not check
- `checking` → `current` when the reply's sequence equals `planSeq` and it succeeded (adopt `result.graph` and the resolved graph without a history step, save the working copy)
- `checking` → `failed` when the reply's sequence equals `planSeq` and it failed (network, 400, 429, 5xx; Retry offered except for 400); nothing is saved
- any reply whose sequence is not `planSeq`, or an aborted call: dropped, no change

**API surface** (no contract change; existing renderer operations under `MP_API_ROOT`):
| Endpoint | Method | Key inputs | Key outputs | Auth | Key errors |
|---|---|---|---|---|---|
| `/v1/catalog` | GET | none | `entries[]` | none (online editor); session in managed mode (#16) | 429 busy, 5xx |
| `/v1/check` | POST | `Graph` body (req) | `CheckResult` (`ok`, `graph`, `bindings`, `problems`) | same | 400 invalid graph, 429 busy, 5xx |
| `/v1/render` | POST | `Graph` body (req) | zip blob | same | 422 with `CheckResult`, 429, 5xx |

**Value sourcing**:
| Action | Value produced / displayed | Source |
|---|---|---|
| Connect drag | consumer, capability | source node id and source handle id `uses:<C>` |
| Connect drag | provider | target node id; valid only if `isValidDrop` |
| Drag highlight | valid target ports | `isValidDrop` over the graph and catalog in the store |
| Inspector need row | required or optional | catalog entry `uses.<C>.optional` (default false) |
| Inspector "Bound to" options | candidates | `candidates(...)` |
| Inspector "Bound to" current value | instance, "Not bound", "Choose a provider", or "<name> (invalid)" | `graph.modules[i].uses[C]`: string (invalid when not in candidates), `null`, missing key (a left out use) |
| Inspector need row | "Recommended: <provider>" and Accept | the `binding_recommended` problem at `modules.<i>.uses.<s>`, current check result only |
| Inspector provides row | who uses it | instances `j` with `graph.modules[j].uses[C] === i` |
| Inspector header | description, "not in catalog" | catalog entry by `graph.modules[i].module` |
| Port and node marks, inspector problems | problems | `result.problems` mapped by `path` (AC-13), via `problemInstance` in `flow.ts` |
| Problems "checking" / "not checked" | check freshness | `planState` |
| Download zip enabled, reason | gate and text | `planState === 'current'` and `result.ok` (no errors; warnings allowed); text by state as in AC-14, N counts `severity: 'error'` problems |
| Failure message, Retry shown | text, retry | `planError.message`; Retry unless `planError.status === 400` |
| Edited marker, replace Dialog | edited | `working.edited` |
| Drop highlight | dragged need | `dragFrom`, set on connect start and cleared on connect end |
| Catalog filter | text | `catalogFilter` in the store |
| Problem centering | zoom | current viewport zoom, at least 1 |
| Storage note | storage failed | `storageFailed`, set when `saveWorking` rejects |
| Node positions | x, y | `WorkingGraph.positions[i]`, else `layout(graph)` from `web/src/graph/layout.ts` |
| Sample copy | file name | `` `${graph.name}.yaml` `` |
| Theme | resolved theme | `mp-theme`, else `prefers-color-scheme` |
| Undo step | previous graph and positions | `history.past` snapshot taken before the edit |
| Busy message | text | `RendererError` message for 429 in `web/src/api/client.ts` |

**Key invariants**:
- The graph in the store is the only source of truth; React Flow nodes and edges are always derived with `toFlow` and never edited in place.
- A graph written by Save as file never contains positions, settings or anything outside the `v0alpha1` shape (spec 0002); its bytes equal `stringifyGraph(graph)`.
- No editor code duplicates check rules beyond `candidates` and `isValidDrop` (the exact name match the renderer also applies); cycles, required needs and recommendations are judged only by the checks.
- The working graph changes only through a user edit (applied at once), an undo or redo, the sample copy, or the check reply for the latest `planSeq` (which replaces it with the renderer's version). A check reply never adds a history step.
- IndexedDB never holds a graph with a check result for a different graph.
- Nothing in #5 adds or removes a module or renames an instance.
- No color literal outside `tokens.css`; no runtime injected `<style>` element and no inline script, because the web module's policy is `script-src 'self'; style-src 'self'` (spec 0001). Style attributes set from React are fine.
- Dimming never lowers text contrast below 4.5:1.

**Canvas wiring** (React Flow props, so the build does not guess):
- Controlled from the store: `nodes` and `edges` come from `toFlow`, with `selected` set from the store's selection; `onNodeClick`, `onEdgeClick` and `onPaneClick` write the selection. `elementsSelectable`, `nodesFocusable` and `edgesFocusable` on; `nodesDraggable` off; `nodesConnectable` on.
- `deleteKeyCode={null}`: React Flow never deletes anything. A `keydown` handler on the canvas wrapper unbinds on Delete or Backspace when a binding is selected and the event target is not a text field, select or other form control.
- New connections: uses handles are `source`, provides handles are `target`, `connectionMode` strict, so a drag can only start at a uses port. `isValidConnection` calls `isValidDrop` with a stable callback that reads the store; `onConnectStart` and `onConnectEnd` set and clear `dragFrom`; `onConnect` calls `bind`.
- Reconnect: `edgesReconnectable` on, with only the target (provider) end reconnectable (`reconnectable: 'target'` on each edge). `onReconnect` calls `bind` with the new provider; a reconnect dropped anywhere else does nothing, and since edges are derived from the graph, the edge stays.
- `ariaLabelConfig` gives nodes, edges and handles readable names ("whoami, module whoami", "whoami uses postgresql from postgres").

**Security model**: no sign in in the online editor; managed mode adds it in #16 at the api, not here. Graph files stay untrusted input (the existing 1 MB limit and strict parse). Catalog descriptions and every graph name render as React text, never as HTML. `mp-theme` is read through an allow list. No new origins: fonts are the system's, icons are bundled. The content policy stays unchanged, and the smoke test keeps failing on any policy violation. No compliance scope.

**Configuration required**: none. No new `MP_*` setting.

**Browser checks** (new): `pnpm --dir web a11y` runs Playwright from `web/a11y/` with two servers and no Docker, at a 1280×800 viewport:
- **dev** (`vite`): axe in both themes (`page.emulateMedia({ colorScheme })` plus the stored `mp-theme`) on the editor states of AC-3 and on `/design`, and the canvas flows.
- **preview** (`vite build` then `vite preview`): the 50 module scale check and the `dist/` marker search for the showcase.

A mock renderer, set up with `page.route`, answers `/v1/catalog` from `web/a11y/fixtures/catalog.json` (the built in entries plus test entries, among them a required need and a description holding HTML). For `/v1/check` it echoes the posted graph, builds `bindings` from its `uses`, and takes `ok` and `problems` from the first named fixture whose predicate matches the posted body (for example "whoami postgresql is null and required" gives `unbound_required`), else none. Each test can delay or fail a reply (503, 429, 400) to drive the latest wins and failure scenarios. The scale graph is built in test code: 50 instances over 10 test catalog entries, 100 bindings. `task test:browser` runs it, and CI runs it in the test job after installing Chromium. `task smoke` gains one connect and disconnect pass on a sample copy against the real renderer, and the theme reload check under the real content policy.

**Critical test scenarios**:
- Happy path: on the sample, unbind whoami's `postgresql` (Delete), which makes the copy; then bind it again by dragging to postgres; each edit checks once, and the file saved afterwards is byte identical to `samples/whoami-postgres/graph.yaml` (the sample binds that need by hand, spec 0002), verifies **AC-9**, **AC-10**, **AC-12**, **AC-16**, **AC-18**
- Keyboard only: Tab to the whoami node, Enter, choose "Not bound" then "postgres" in the inspector select, then undo twice with the keyboard, verifies **AC-11**, **AC-15**
- Latest wins: two edits in a row with the first check reply delayed past the second; only the second reply is applied, verifies **AC-12**
- Failure case: the check route answers 503, then 429; the edit stays, problems show "not checked", Download zip is disabled with its reason, Retry recovers, verifies **AC-14**
- Required unbind: unbind a required need in a fixture graph; `unbound_required` appears on that port and download is blocked, verifies **AC-10**, **AC-13**, **AC-14**
- Problem navigation: click a problem in the panel; its module is selected and centered, verifies **AC-13**
- Sample protection: edit the sample while the working graph is edited; the Dialog appears; Cancel keeps both untouched, Replace copies, verifies **AC-18**
- Accessibility: axe in light and dark on the five editor states and `/design`, zero violations, verifies **AC-3**
- Theme: in `task smoke` (real Caddy and content policy), set Dark, reload, and at `DOMContentLoaded` (before the deferred app module runs) `<html>` already has `data-theme="dark"`; in the browser check, storage that throws still lets the toggle work, verifies **AC-2**
- Undo against replies: bind, then undo while the bind's check is still delayed; the late reply is dropped and the graph equals the snapshot, verifies **AC-12**, **AC-15**
- Selection loss: select a binding, press Delete; the consumer module is selected and focused, verifies **AC-8**, **AC-10**
- Scale: a generated 50 module, 100 binding graph meets the timing and the render count check, verifies **AC-7**
- Hostile text: a catalog fixture description containing `<img src=x onerror=alert(1)>` shows as text, verifies **AC-20**
- Permission: none applies in #5 (no roles exist yet); #16 owns who may check and deploy

## Build plan

Tracer Bullet: the first task runs one thin strand through every layer of this feature (tokens, shell, canvas, edit, renderer, storage, browser check); each later task thickens one strand.

1. [x] Thin thread: create `tokens.css` (both themes, values above), `base.css` and `reactflow.css`; `public/theme.js` with its `index.html` tag and the `no-cache` rule in `web/Caddyfile`, `theme.ts` and ThemeToggle; AppShell with the four regions and the 1024px stack; restyle `GraphView` and `ModuleNode` (geometry constants in `flow.ts` as CSS properties, smooth step edges, dot grid, controls); `edit.ts` with `bind` and `isValidDrop`; the store's `planSeq`, `planState` and `positions`; drag connect on the working graph through one check call, adopt the reply, save to IndexedDB; set up `web/a11y/` with fixtures, `pnpm --dir web a11y`, `task test:browser` and the CI step, with the axe check on the sample in both themes, satisfies **AC-2**, **AC-3**, **AC-6**, **AC-7**, **AC-9**, **AC-12**, **AC-16**
2. [x] Rebind by dragging an edge end, `unbind` through Delete or Backspace (nodes never deleted), unit tests for the edit functions, satisfies **AC-9**, **AC-10**
3. [x] Selection model and highlight with dimming; the Inspector with Select, `candidates`, `acceptRecommendation`, Remove binding, who uses each offer and the recommendation note; keyboard flow in the browser check, satisfies **AC-8**, **AC-11**
4. [x] Problem marks on ports and nodes, the ProblemsPanel with select and center, "checking" and "not checked", the Failure with Retry, the download gate with its reason, satisfies **AC-13**, **AC-14**
5. [x] Undo and redo (shortcuts and buttons), Tidy layout, the edited marker, the storage note, Dialog plus the replace prompt on Open file, satisfies **AC-15**, **AC-16**, **AC-17**, **AC-18**
6. [x] Sample route: hint, first edit copies and navigates, the replace prompt, satisfies **AC-18**
7. [x] CatalogPanel with SearchField filter, empty state and Find providers, satisfies **AC-19**
8. [x] Unknown module nodes and plain text rendering checks, satisfies **AC-20**
9. [x] Complete the component set (Badge, StatusBadge, Tooltip, IconButton, Toolbar, EmptyState, Failure restyle), the dev only `/design` route with its bundle marker test, the color literal test, remove `app.css`, write `web/design.md`, satisfies **AC-1**, **AC-4**, **AC-5**
10. [x] Finish the browser checks: axe on every listed state and `/design` in both themes, the latest wins and failure scenarios, the 50 module scale check; add the connect and disconnect pass to `task smoke`, satisfies **AC-3**, **AC-7**, **AC-12**, **AC-14**

## Consequences

**Positive**:
- Every later screen (#16 review and module state, #6 place, delete and rename) starts from documented tokens and accessible components instead of inventing styles.
- Edits as pure functions plus one history give #6 its undo and its edit model for free.
- The renderer stays the only judge of graph rules, so the editor cannot drift from it.

**Negative / tradeoffs**:
- React Aria Components is a larger dependency than a lighter primitive set, and its styling idiom (data attributes such as `[data-focus-visible]`, render props) is one more thing to learn.
- One check call per edit means the editor depends on the renderer for feedback; offline or rate limited, edits stay unchecked and download is blocked.
- Positions live only in the browser: another browser or a cleared storage shows the auto layout again.
- System fonts look slightly different on each OS, so pixel screenshot tests are ruled out; visual changes need a human look.
- The browser check adds Chromium to the CI test job, a few minutes per run.
- Dev only `/design` means the showcase is not visible on a deployed instance.

**Neutral**:
- `web/src/app.css` goes away; the existing class names in tests and the smoke spec (`.react-flow__edge`, `node-<instance>` test ids) must keep working or be updated together.
- `WorkingGraph` gains an optional field; old stored entries still load.
- `@xyflow/react` 12.12.1 is out (the project pins 12.12.0); a patch bump fits this work.

## Follow-up

- [ ] `/sync`: in `web/AGENTS.md`, replace "Styling and components: decided in spec #5, not yet" with CSS Modules plus tokens, React Aria Components and Lucide, and link `web/design.md` and this spec; add `design-system-patterns` and `playwright-best-practices` under `## Agent skills` (web area, not root); add the Playwright MCP to an `MCP servers:` line; add `pnpm --dir web a11y` to its commands.
- [ ] Connect the Playwright MCP yourself (for example `claude mcp add playwright -- npx @playwright/mcp@latest`), so the build can drive the canvas and `/design` in a real browser.
- [ ] At build time, confirm that the installed axe core accepts the `wcag22aa` tag; if not, drop it from AC-3's run and note it in `design.md`.
- [ ] Each edit sends one check call (aborting the one before), so a fast burst of edits against the rate limited public renderer can still get a 429; watch for it when the online editor returns (#22).
- [ ] #16: build review on Dialog, module state on StatusBadge, and its read only stack view on this canvas (the read only mode was left to #16).
- [ ] #6: reuse `edit.ts`, the history and the Dialog for placing, deleting and renaming; add multi select and dragging from the catalog panel.
