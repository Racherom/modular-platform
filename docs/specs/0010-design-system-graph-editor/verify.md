# Verify: Design system and graph editor foundation · spec 0010 · updated 2026-11-04
_Steps derived from spec 0010 acceptance criteria. `/check verify` runs these; `/test` locks the durable ones._

Milestone 1, thin thread (build plan task 1). Later milestones append their steps below.

## UI / manual
- [ ] With no `mp-theme` stored, switch the OS between light and dark → the editor and the canvas follow at once; the toggle shows System → AC-2
- [ ] Choose Dark in the header toggle, reload → the page paints dark from the first frame (no light flash), the canvas is dark, `localStorage['mp-theme']` is `dark` → AC-2
- [ ] Set `localStorage['mp-theme']` to `purple`, reload → System is selected and the OS setting decides → AC-2
- [ ] Block site storage in the browser, use the toggle → the theme still changes for this session → AC-2
- [ ] At 1280×800 open a graph → header (app name, file name, toolbar, theme toggle), catalog left, canvas middle, inspector right, problems below the canvas → AC-6
- [ ] Press Hide on the problems panel, then Show → the body collapses and comes back; the button reports `aria-expanded` → AC-6
- [ ] Narrow the window to 1000px, then 360px → the panels stack under the canvas (problems, inspector, catalog), the graph still shows, no horizontal page scroll → AC-6
- [ ] Look at a node → instance name and module, provides ports left, uses ports right; a required need has a filled handle, an optional need a hollow dashed one; bindings are smooth step edges with the arrow at the provider; dot grid and zoom in, zoom out and fit controls → AC-7
- [ ] Try to drag a node → it does not move → AC-7
- [ ] Open a graph with `whoami.uses.postgresql: null`, drag from whoami's `postgresql` port → postgres's `postgresql` port gets an accent ring, every other port dims → AC-9
- [ ] Drop it there → the edge appears at once, "Edited" shows beside the file name, the problems panel says "Checking the graph…" until the reply, then the reply's graph is shown; exactly one `POST /v1/plan` in the network panel → AC-9, AC-12
- [ ] Drop a need on empty canvas, on the module's own offer, or on an offer with another capability name → nothing changes and no plan call is sent → AC-9
- [ ] Start a drag at a provides port → no connection line starts → AC-9
- [ ] After a bind, the nodes stay exactly where they were → AC-16
- [ ] Reload after a bind → the edge, the positions and "Edited" come back from IndexedDB with no new plan call → AC-12, AC-16, AC-17
- [ ] Make two binds quickly while the first plan reply is slow (throttle the network) → only the second binding stays → AC-12
- [ ] Make the plan call fail (offline, or a 503) → the edge stays, the problems panel shows "Not checked" and a Failure with Retry; Download zip is disabled; Retry plans again and clears it → AC-12
- [ ] Run axe (or the browser check) in light and in dark on the sample, on the editor with a graph, and during a connect drag → zero violations; every focusable element shows a visible focus ring → AC-3
- [ ] Save as file after an edit → the file holds exactly the graph text, no positions; "Edited" disappears → AC-16, AC-17

## Value sourcing
- [ ] Connect: consumer and capability come from the dragged source handle `uses:<C>`; drag from a module with two needs and check that only the dragged need changes → AC-9
- [ ] Connect: the provider comes from the drop target node and is accepted only when `isValidDrop` agrees; with two providers of the same capability, drop on the second and check the binding names it → AC-9
- [ ] Drag highlight: valid ports come from `isValidDrop` over the graph and catalog; an instance whose module is not in the catalog never lights up → AC-9
- [ ] Node positions: from the working copy, else the auto layout; remove one instance's position from IndexedDB, reload, and the whole graph is laid out again → AC-16
- [ ] Theme: `mp-theme`, else `prefers-color-scheme`; vary both and check the resolved theme on `<html>` and on `.react-flow` → AC-2
- [ ] Problems "checking" and "not checked": from the plan state; delay and fail the reply and watch the label → AC-12
- [ ] Download zip enabled: only while the plan is current; it is disabled while checking and after a failure → AC-12 (the full gate with reasons is AC-14)
- [ ] Failure text: the planner's message; a 429 says "The planner is busy, try again in a minute." → AC-12
- [ ] Edited marker: `working.edited`, set by an edit, kept over reload, cleared by Save as file → AC-17

## Commands
- [ ] `pnpm --dir web test` → all Vitest suites pass, including `edit.test.ts`, `store.test.ts`, `theme.test.ts` and the `withPositions` cases → AC-2, AC-9, AC-12, AC-16
- [ ] `task test:browser` (or `pnpm --dir web a11y`) → 21 checks pass: axe in both themes, connect, latest reply wins, failure and Retry, theme and layout → AC-2, AC-3, AC-6, AC-9, AC-12, AC-16
- [ ] `task lint` → Biome, tsc and golangci-lint clean
- [ ] `pnpm --dir web build` → `dist/theme.js` exists, `dist/index.html` loads it before the bundle, and `routeTree.gen.ts` is unchanged → AC-2
- [ ] `task smoke` → the online tool still shows the catalog, downloads the sample zip and works under `/platform`, with no content policy violation → AC-2

## Acceptance criteria coverage
- AC-2: theme steps, value sourcing (theme), build command · AC-3: axe step and browser check (sample, open graph, connect drag; selection, problems and `/design` come later) · AC-6: layout steps · AC-7: node look and no drag (scale timing and render count come in task 10) · AC-9: connect, drop elsewhere, provides start, value sourcing · AC-12: plan steps, latest wins, failure · AC-16: positions and reload · AC-17: partly (marker, reload, save; the storage note comes in task 5)
