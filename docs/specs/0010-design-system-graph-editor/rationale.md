# 0010. Design system and graph editor foundation: rationale

Decision record for [index.md](index.md). Read by humans and by `/architect` on update or supersede; `/develop` skips it.

## Context

The editor in `web/` is a React 19 single page app on Vite, with React Flow 12 for the canvas, Zustand for editor state and TanStack Query for planner calls (spec 0001, `web/AGENTS.md`). Its look is a placeholder: one global `app.css` with eight color variables, marked "until the design system (feature #5)". Spec 0001 left styling and components open on purpose. The canvas is read only: nodes cannot be connected, edges cannot be removed, and the only way to change a binding is to edit `graph.yaml` by hand and open it again.

Several features wait on this. #16 (managed mode) needs plan review, apply progress and module state on the graph; spec 0004 lets it start on the current styling, but every screen it builds that way is later restyled. #6 adds placing, deleting and renaming modules and needs an edit model, undo and a palette to drag from. #24 runs the same bundle under a sub path, so nothing may depend on the site root, and the web module serves a strict content policy (`script-src 'self'; style-src 'self'`) that rules out inline scripts and injected style elements.

The forces: a small open source team, so few dependencies and little upkeep; WCAG AA with a keyboard path to every control, the canvas included (`web/AGENTS.md`); self hosted and offline friendly installs, so no third party font or icon hosts; one source of graph rules (`libs/planner`, per the DDD rules in `AGENTS.md`), so the editor must not grow a second planner; and graph files without positions (spec 0002), so a moved node never changes the file.

Not deciding means each feature styles its own screens, accessibility is retrofitted screen by screen, and #6 invents its edit model under time pressure.

## Options considered

The decision has two parts: how the design system is built (A), and how the canvas holds and edits the graph (B).

### Option A1: CSS Modules over one token file, React Aria Components, Lucide (chosen)

Tokens as CSS custom properties in one file; each component styles itself in a `.module.css` file; interactive parts (select, dialog, tooltip, toolbar, toggle group) come from React Aria Components, which ships behavior and no styles; icons from `lucide-react`; system fonts. (basis: `web/AGENTS.md` stack; Vite's built in CSS Modules support; WCAG 2.2 AA)

**Pros**:
- No new build tooling: Vite handles CSS Modules natively, and React Flow is themed through its own `--xy-*` variables.
- React Aria Components brings the most complete keyboard, focus and screen reader behavior, which is the costliest part to get right by hand.
- Works under `style-src 'self'`: no runtime style injection.

**Cons**:
- React Aria Components is larger than the alternatives, and its state styling through data attributes is new to the team.
- CSS Modules give scoping but no type checked token names; a typo in `var(--color-txt)` fails silently.

### Option A2: Tailwind CSS v4 with copied in Radix based components

Utility classes with tokens in Tailwind's `@theme`, and components copied into the repo in the shadcn/ui style on Radix Primitives.

**Pros**:
- Very fast to build screens; a large community and many ready made component recipes.
- Tokens and utilities come from one config.

**Cons**:
- A large shift from today's CSS, plus a build plugin; long class strings in custom React Flow nodes are hard to read.
- Radix Primitives upkeep has slowed as its team moved on to Base UI.
- Copied components become our code to maintain.

### Option A3: A styled component kit (for example Mantine)

A complete styled library with its own theme system.

**Pros**:
- The most components on day one, already accessible and themed.

**Cons**:
- Its look is the kit's look; a calm, custom technical style means fighting its theme.
- Larger bundle and a framework sized dependency for a small app.
- Some kits inject styles at runtime, which the content policy blocks.

### Option A4: Plain global CSS and hand rolled components

Keep the current approach: one stylesheet, hand written components.

**Pros**:
- Zero dependencies; the simplest thing that works today.

**Cons**:
- Accessible select, dialog, tooltip and toolbar behavior becomes our own work and our own bugs.
- Global class names clash as screens grow.

### Option B1: Canvas controlled from the graph (chosen)

The graph in the Zustand store is the only truth. React Flow nodes and edges are always derived from it (`toFlow`); drag gestures call pure edit functions that return a new graph; the planner checks every edit and its reply replaces the graph.

**Pros**:
- One source of truth, so the file, the canvas and the plan cannot drift apart.
- Pure edit functions are easy to unit test and give undo, redo and #6's edits one shape.
- Graph rules stay in `libs/planner`.

**Cons**:
- Every edit derives the flow again; it needs memoized nodes to stay fast at 50 modules.
- Feedback depends on a planner call after each edit.

### Option B2: React Flow owns nodes and edges

Use React Flow's own node and edge state (`useNodesState`, `useEdgesState`) as the working model and convert it back to a graph on save or plan.

**Pros**:
- The pattern most React Flow examples show; gestures update state with no extra code.

**Cons**:
- Two models of the same graph that must be synced both ways; bindings, `null` and missing keys (spec 0002) have no natural home in edge state.
- Undo and planner replies must patch React Flow's state instead of replacing a value.

## Rationale

A1 wins on the forces that matter most here. Accessibility is a stated project rule, and the parts that are hard to make accessible (select with a listbox, modal dialog, tooltip, toolbar with arrow keys) are exactly what React Aria Components does best; building them by hand (A4) puts that risk on a small team. A2 and A3 buy speed with a large style system and, for A3, a fight with someone else's look plus a possible clash with the content policy. CSS Modules plus custom properties are what the browser and Vite already do, so the day 730 cost is close to zero. The missing type safety for token names is handled by a small test that keeps color literals out of component CSS and by `design.md` as the reference. (basis: `web/AGENTS.md` accessibility rule; spec 0001 content policy; `design-system-patterns` skill, tokens and theming)

B1 follows the project's own rules: the graph is the aggregate, the planner owns the rules, and the editor is an edge that maps input to the domain (`AGENTS.md`, DDD rules). Spec 0002's three states for a need (bound, `null`, missing) fit a graph naturally and React Flow's edge list poorly, which is the decisive point against B2. The render cost of deriving the flow on each edit is small at the target of 50 modules and is guarded by a scale check. (basis: `AGENTS.md` pure domain rule; spec 0002 binding states; `react-flow` and `react-state-management` skills)

The engineer's choices in this session shaped the rest: calm technical direction, blue accent, system fonts, a system default theme with a toggle, comfortable density, the three panel shell, connect and disconnect but no node moving, the inspector "Bound to" select as the keyboard path, exact match drops, `null` on unbind with download blocked while an error stands, a plan after each edit with the latest reply winning, stable positions with a Tidy button, port level problems, undo now, a sample that copies on first edit, no read only mode yet, a 50 module target, a dev only `/design` route, `web/design.md`, and axe checks without screenshots. Decided here as recommended items: the replace Dialog when an edited working graph would be overwritten (otherwise a first edit on the sample could silently drop unsaved work; runner up: always replace and rely on undo, which does not survive a reload); a same origin start script for the theme (the content policy blocks inline scripts; runner up: setting the theme in the app bundle, which flashes the system theme first); dimming only edges and borders (dimming text with opacity fails AA contrast; runner up: lighter text tokens for unrelated nodes, more tokens for little gain); a browser check against the dev server with mocked planner routes (no Docker in CI; runner up: running it inside `task smoke`, which is manual only while the online tool is deferred, spec 0004); and the "bound by the planner" note limited to the current plan (honest and simple; runner up: storing an auto flag, which would add a field to the graph that spec 0002 does not allow).

## References

**Project sources**:
- `AGENTS.md`: DDD rules, pure domain, contract first, ubiquitous language
- `web/AGENTS.md`: stack, conventions, accessibility rule, no `<base href>`
- Spec [0001](../0001-platform-stack-architecture/index.md): web module content policy, base path handling, "styling decided in #5"
- Spec [0002](../0001-platform-stack-architecture/0002-provisional-graph-format.md): graph format, no positions, binding states, candidate rule, problem paths
- Spec [0004](../0001-platform-stack-architecture/0004-managed-mode-first.md): #16 screens, online tool smoke out of CI
- Installed skills: `react-flow`, `react-state-management`, `design-system-patterns`, `playwright-best-practices`
- Research cache: `docs/.agent-cache/research/design-system-graph-editor.md` (2026-11-03), `docs/.agent-cache/tool-discovery/design-system-graph-editor.md`

**Practices & standards**:
- WCAG 2.2 level AA: 1.4.3 contrast minimum, 1.4.11 non text contrast, 2.4.7 focus visible, 2.4.11 focus not obscured (focus appearance, 2.4.13, is AAA and not required)
- Single source of truth with derived views (unidirectional data flow)
- Design tokens as the only place for raw values

**Links** (web verified on 2026-11-03):
- React Aria Components: https://react-aria.adobe.com/ (styling: https://react-aria.adobe.com/styling), version 1.22.1
- Lucide: https://lucide.dev, `lucide-react` 1.67.0
- axe core for Playwright: https://github.com/dequelabs/axe-core-npm, `@axe-core/playwright` 4.14.0
- React Flow theming: https://reactflow.dev/learn/customization/theming
- React Flow accessibility: https://reactflow.dev/learn/advanced-use/accessibility
- React Flow dark mode example: https://reactflow.dev/examples/styling/dark-mode
- WCAG 2.2 contrast minimum: https://www.w3.org/WAI/WCAG22/Understanding/contrast-minimum.html
- Non text contrast (the WCAG 2.1 page was the one returned): https://www.w3.org/WAI/WCAG21/Understanding/non-text-contrast.html
- WCAG 2.2 focus not obscured: https://www.w3.org/WAI/WCAG22/Understanding/focus-not-obscured-minimum.html
- Vite CSS Modules: https://vite.dev/guide/features#css-modules
