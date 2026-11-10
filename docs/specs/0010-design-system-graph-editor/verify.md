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
- [ ] Drop it there → the edge appears at once, "Edited" shows beside the file name, the problems panel says "Checking the graph…" until the reply, then the reply's graph is shown; exactly one `POST /v1/check` in the network panel → AC-9, AC-12
- [ ] Drop a need on empty canvas, on the module's own offer, or on an offer with another capability name → nothing changes and no check call is sent → AC-9
- [ ] Start a drag at a provides port → no connection line starts → AC-9
- [ ] After a bind, the nodes stay exactly where they were → AC-16
- [ ] Reload after a bind → the edge, the positions and "Edited" come back from IndexedDB with no new check call → AC-12, AC-16, AC-17
- [ ] Make two binds quickly while the first check reply is slow (throttle the network) → only the second binding stays → AC-12
- [ ] Make the check call fail (offline, or a 503) → the edge stays, the problems panel shows "Not checked" and a Failure with Retry; Download zip is disabled; Retry checks again and clears it → AC-12
- [ ] Run axe (or the browser check) in light and in dark on the sample, on the editor with a graph, and during a connect drag → zero violations; every focusable element shows a visible focus ring → AC-3
- [ ] Save as file after an edit → the file holds exactly the graph text, no positions; "Edited" disappears → AC-16, AC-17

## Value sourcing
- [ ] Connect: consumer and capability come from the dragged source handle `uses:<C>`; drag from a module with two needs and check that only the dragged need changes → AC-9
- [ ] Connect: the provider comes from the drop target node and is accepted only when `isValidDrop` agrees; with two providers of the same capability, drop on the second and check the binding names it → AC-9
- [ ] Drag highlight: valid ports come from `isValidDrop` over the graph and catalog; an instance whose module is not in the catalog never lights up → AC-9
- [ ] Node positions: from the working copy, else the auto layout; remove one instance's position from IndexedDB, reload, and the whole graph is laid out again → AC-16
- [ ] Theme: `mp-theme`, else `prefers-color-scheme`; vary both and check the resolved theme on `<html>` and on `.react-flow` → AC-2
- [ ] Problems "checking" and "not checked": from the resolved graph state; delay and fail the reply and watch the label → AC-12
- [ ] Download zip enabled: only while the resolved graph is current; it is disabled while checking and after a failure → AC-12 (the full gate with reasons is AC-14)
- [ ] Failure text: the renderer's message; a 429 says "The renderer is busy, try again in a minute." → AC-12
- [ ] Edited marker: `working.edited`, set by an edit, kept over reload, cleared by Save as file → AC-17

## Commands
- [ ] `pnpm --dir web test` → all Vitest suites pass, including `edit.test.ts`, `store.test.ts`, `theme.test.ts` and the `withPositions` cases → AC-2, AC-9, AC-12, AC-16
- [ ] `task test:browser` (or `pnpm --dir web a11y`) → 21 checks pass: axe in both themes, connect, latest reply wins, failure and Retry, theme and layout → AC-2, AC-3, AC-6, AC-9, AC-12, AC-16
- [ ] `task lint` → Biome, tsc and golangci-lint clean
- [ ] `pnpm --dir web build` → `dist/theme.js` exists, `dist/index.html` loads it before the bundle, and `routeTree.gen.ts` is unchanged → AC-2
- [ ] `task smoke` → the online editor still shows the catalog, downloads the sample zip and works under `/platform`, with no content policy violation → AC-2

## Acceptance criteria coverage
- AC-2: theme steps, value sourcing (theme), build command · AC-3: axe step and browser check (sample, open graph, connect drag; selection, problems and `/design` come later) · AC-6: layout steps · AC-7: node look and no drag (scale timing and render count come in task 10) · AC-9: connect, drop elsewhere, provides start, value sourcing · AC-12: check steps, latest wins, failure · AC-16: positions and reload · AC-17: partly (marker, reload, save; the storage note comes in task 5)

---

Milestone 2, editing (build plan tasks 2 to 4), added 2026-11-04.

## UI / manual
- [ ] Open a graph with postgres instances `a` and `b` and `whoami.uses.postgresql: a`; drag the arrow end of the edge to `b` → `b`'s port gets the accent ring during the drag; on drop the edge reads "whoami uses postgresql from b" and one `POST /v1/check` is sent → AC-9
- [ ] Drag the arrow end of an edge onto empty canvas → the edge snaps back unchanged and no check call is sent; the consumer end of an edge cannot be grabbed → AC-9
- [ ] Click an edge → it turns accent, both of its ports light up, the inspector shows "Binding"; press Delete (or Backspace) → the edge is gone, `whoami` is selected and has keyboard focus, every module is still there → AC-8, AC-10
- [ ] With a module selected, press Delete or Backspace → nothing changes; type Backspace inside the "Bound to" select or any text field → nothing is unbound → AC-10
- [ ] Select a binding and press the inspector's Remove binding → same result as Delete → AC-10
- [ ] Click a module → 2px accent border, its edges and ports accent, unrelated edges thin and dimmed, unrelated node borders dimmed while every text stays readable; a node with an error keeps its red border → AC-8
- [ ] Press Escape, or click empty canvas → the selection clears and nothing is dimmed → AC-8
- [ ] Keyboard only: Tab to the `whoami` node, press Enter → the inspector lists the need `postgresql` with "optional", and Provides `http` with "Not used"; open "Bound to" with Enter → options `a`, `b`, "Not bound", "Choose a provider" (disabled); choose "Not bound" then `b` → the edge goes away, then comes back from `b`, one check call each → AC-8, AC-11
- [ ] Select `b` → its Provides row says "Used by whoami" → AC-11
- [ ] Leave a required need out with one candidate → the field shows "Recommended: <provider>" and Accept; press Accept → the select shows that instance and the next check call carries the binding → AC-11
- [ ] Open a file where `whoami.uses.postgresql` names `whoami` itself → the select shows "whoami (invalid)" → AC-11
- [ ] Open a graph where `test-app`'s required `postgresql` is `null` → a red circle mark sits on that port, its accessible name is the `unbound_required` message; Download zip is disabled with "Fix 1 error to download" beside it → AC-10, AC-13, AC-14
- [ ] A problem whose path is `modules.<i>` (not a need), or a need problem on a module that is not in the catalog → the mark sits in the node header instead of a port; a graph level problem shows only in the panel → AC-13
- [ ] Give one port a warning and an error → one red mark whose name lists both messages → AC-13
- [ ] Click a problem in the problems panel → its module is selected, centered at the current zoom (at least 1), and focused; with reduced motion on, the canvas jumps without animating → AC-13
- [ ] Make the check call answer 400 → the edit stays, "Not checked", the renderer's message, and no Retry button; answer 429 → "The renderer is busy, try again in a minute." with Retry → AC-14
- [ ] Watch the reason beside Download zip → "Checking the graph" while checking, "Not checked, retry first" after a failure, "Fix N errors to download" with errors only; warnings alone never disable it; in managed mode Deploy follows the same rule → AC-14

## Value sourcing
- [ ] Inspector "required" or "optional": from the catalog entry's `uses.<C>.optional`; flip it in the fixture catalog and the label follows → AC-11
- [ ] "Bound to" options: from `candidates`; add a third postgres instance and it appears, sorted by name; an instance whose module is not in the catalog and the consumer itself never appear → AC-11
- [ ] "Bound to" current value: a string in the candidates shows that name, a string outside shows "(invalid)", `null` shows "Not bound", a missing key shows "Choose a provider" → AC-11
- [ ] "Recommended": only for a `binding_recommended` problem while the check result is current; make the next check call slow and the note disappears while checking → AC-11
- [ ] "Used by": the instances whose need for that capability names this instance; rebind and the list follows → AC-11
- [ ] Marks: from `result.problems` by `path`; change a path in a mocked reply from `modules.app.uses.postgresql` to `modules.app` and the mark moves from the port to the header → AC-13
- [ ] Download reason: N counts only `severity: error`; a reply with two errors and a warning says "Fix 2 errors to download" → AC-14
- [ ] Retry shown: hidden only for `status === 400` → AC-14

## Commands
- [ ] `pnpm --dir web test` → 118 tests pass, including `edit.test.ts` (unbind, acceptRecommendation, candidates), `selection.test.ts` and `flow.test.ts` (marks, emphasis, download gate) → AC-8, AC-9, AC-10, AC-11, AC-13, AC-14
- [ ] `pnpm --dir web a11y` → 36 checks pass, including `editing.spec.ts` and axe in both themes with a module selected, a binding selected with the select open, and problems shown → AC-3, AC-8 to AC-14

## Acceptance criteria coverage
- AC-8: click, Enter, Escape, pane click, dimming, selection after unbind · AC-9: reconnect, reconnect dropped elsewhere · AC-10: Delete, Backspace, Remove binding, never a module, required unbind · AC-11: inspector rows, options, invalid, recommendation note, keyboard flow · AC-13: port and node marks, error wins, panel navigation · AC-14: 400 without Retry, 429, download reason · AC-3: two more editor states (the connect drag and `/design` axe runs complete in task 10)

Milestone 3, session (build plan tasks 5 to 8), added 2026-11-04.

## UI / manual
- [ ] Open a graph, bind by dragging, bind again from the inspector, then press Undo twice → each press steps back one edit, each sends one check call, and the Edited marker stays on → AC-15, AC-17
- [ ] Press Ctrl+Z, Ctrl+Shift+Z and Ctrl+Y with focus on the canvas (Cmd+Z and Cmd+Shift+Z on macOS) → undo, redo, redo; the Undo and Redo buttons disable when there is nothing left → AC-15
- [ ] Type in the catalog filter, or focus the inspector "Bound to" select, then press Ctrl+Z → the graph does not change → AC-15
- [ ] Bind, then undo while the bind's check call is still slow → the late reply is dropped and the graph stays as before the bind → AC-12, AC-15
- [ ] Undo once, then make a new edit → Redo is disabled → AC-15
- [ ] Open a file after some edits → Undo and Redo are disabled and the Edited marker is gone → AC-15, AC-17
- [ ] Move a node's stored position (IndexedDB `modular-platform` / `working` / `graph`), reload, press Tidy layout → nodes go back to the auto layout, no check call is sent, Edited shows; Undo moves them back → AC-15, AC-16
- [ ] Save as file after edits → the file has no positions and the Edited marker clears; reload → the marker stays cleared → AC-16, AC-17
- [ ] Block IndexedDB (or fill the quota) and open a graph → a quiet note says "Changes are not kept after reload in this browser." → AC-17
- [ ] Visit `/samples/whoami-postgres` → a hint says the first change makes a copy; Undo, Redo and Tidy layout are disabled → AC-18
- [ ] On the sample with no edited working graph, select the binding and press Delete → the editor root opens with file name `whoami-postgres.yaml`, Edited on, the binding gone, whoami selected and focused, one check call; one Undo brings the binding back and disables Undo → AC-8, AC-18
- [ ] On that copy, drag whoami's postgresql to postgres again and Save as file → the file is byte identical to `samples/whoami-postgres/graph.yaml`; the sample page still shows its binding → AC-16, AC-18
- [ ] With an edited working graph, reload the sample and press Delete on its binding → the "Replace the edited graph?" dialog opens with Cancel focused and nothing on screen changed; Cancel (or Escape) keeps both graphs and the URL; an inspector choice cancelled the same way reverts the select → AC-18
- [ ] In that dialog choose Save as file → the working graph downloads under its file name, then the copy is made; choose Replace → the copy is made without a download → AC-18
- [ ] With an edited working graph press Open file → the dialog opens before any file chooser; Escape returns focus to Open file and no chooser opens; Replace opens the chooser → AC-18
- [ ] Choose Find providers on a need in the inspector → the catalog filter holds that capability and has focus; below 1024px the stacked catalog panel scrolls into view → AC-19
- [ ] Type `HTTP` in the filter → only entries whose name, description or capability contains "http" stay, the count says "2 of 4 modules"; type `redis` → "No module matches" with Clear the filter → AC-19
- [ ] Open a graph with an instance whose module is not in the catalog → its node shows a "not in catalog" badge and no ports, it never lights up as a drop target and never appears as a "Bound to" option → AC-20
- [ ] Look at the `test-hostile` catalog entry and a graph with an HTML module name → both show as literal text; no image, no alert → AC-20

## Value sourcing
- [ ] Undo step: from the `history.past` snapshot taken before the edit; after a check reply fills in a default, undo of the next edit restores the filled in graph, not the file as typed → AC-15
- [ ] Node positions: from `WorkingGraph.positions`, else `layout(graph)`; delete one instance's stored position and reload → the whole graph gets the auto layout → AC-16
- [ ] Edited marker and the replace dialog: from `working.edited`; set it to false in IndexedDB, reload the sample and edit → no dialog → AC-17, AC-18
- [ ] Sample copy file name: `` `${graph.name}.yaml` ``; change the sample graph's `name` and the copy's file name follows → AC-18
- [ ] Catalog filter text: from `catalogFilter` in the store; it survives switching between selections and screens in one session → AC-19
- [ ] Storage note: from `storageFailed`, set when `saveWorking` rejects; a later save that works clears the note → AC-17

## Commands
- [ ] `pnpm --dir web test` → 151 tests pass, including `history.test.ts` (undo, redo, Tidy, sample copy), `keys.test.ts` (shortcuts) and `catalog-panel.test.tsx` (filter, empty state, Find providers, hostile text) → AC-15 to AC-20
- [ ] `pnpm --dir web a11y` → 44 checks pass, including `session.spec.ts` (8 flows) and the axe runs in both themes → AC-3, AC-15 to AC-20

## Acceptance criteria coverage
- AC-15: buttons, shortcuts, form control guard, redo cleared, undo while checking · AC-16: Tidy without a check, positions never saved · AC-17: Edited after undo, cleared by save and open, storage note · AC-18: hint, disabled history, copy and navigate, dialog Cancel, Save as file, Replace, Open file prompt · AC-19: filter fields, empty state, Find providers focus and scroll · AC-20: badge, no ports, never a candidate, plain text

Milestone 4, design system complete (build plan tasks 9 and 10), added 2026-11-04.

## UI / manual
- [ ] Run `pnpm --dir web dev` and open `/design` → every token group (color swatches with light and dark values, type, spacing, radius, shadow, layers, motion, layout) and every component of AC-4 in every listed state, with a theme switch on the page → AC-4, AC-5
- [ ] On `/design`, focus Undo in the "History, example" toolbar and press the arrow keys → focus moves between Undo, Redo and Tidy layout; the next Tab leaves the toolbar → AC-4
- [ ] On `/design`, open the Dialog and press Escape → it closes and focus returns to "Open the dialog"; Tab inside it never leaves it → AC-4
- [ ] Hover or focus an IconButton → its Tooltip shows its accessible name; StatusBadge always shows a dot and a word, never color alone → AC-4
- [ ] Tab through the editor in light and in dark, including a node and an edge → every element shows a visible focus ring (outline, or a thicker edge stroke in the focus color) → AC-3
- [ ] Run `pnpm --dir web build && pnpm --dir web preview` and open `/design` → the "Page not found" screen → AC-5
- [ ] Compare each token table in `web/design.md` with `web/src/styles/tokens.css` → every token is listed with its property name and its light and dark values; the canvas grammar section matches the canvas → AC-1, AC-7

## Value sourcing
- [ ] Colors: only from `web/src/styles/tokens.css`; put `color: #f00` in any other CSS file under `web/src/` → `color-literals.test.ts` fails → AC-1
- [ ] Focus ring color: `--color-focus` from `tokens.css`; set it to the surface color → `focus-contrast.test.ts` fails in that theme → AC-3
- [ ] `/design` availability: `import.meta.env.DEV`; in a production build the route throws `notFound()` and `dist/` holds no `mp-design-showcase` marker → AC-5

## Commands
- [ ] `pnpm --dir web test` → 176 tests pass, including `color-literals.test.ts`, `focus-contrast.test.ts` and `render-count.test.tsx` → AC-1, AC-3, AC-7
- [ ] `pnpm --dir web a11y` → 53 checks pass: axe in both themes on the 9 states of `axe.spec.ts`, `preview/bundle.spec.ts` (marker search and not found), `preview/scale.spec.ts` (50 modules, 100 bindings, under 100 ms), `preview/policy.spec.ts`, `toolbar.spec.ts`, and the latest wins and failure flows in `connect.spec.ts` → AC-3, AC-4, AC-5, AC-7, AC-12, AC-14
- [ ] `task smoke` (ports 80 and 443 free) → green, including "unbind and bind again on a sample copy" and "a stored Dark theme is on `<html>` before the app module runs, under the content policy" → AC-2, AC-9, AC-10, AC-12, AC-18

## Acceptance criteria coverage
- AC-1: token tables, color literal test · AC-3: axe on every listed state in both themes, focus contrast test, manual focus pass · AC-4: `/design`, toolbar arrows, Dialog focus, Tooltip, StatusBadge · AC-5: dev showcase, production not found, bundle marker · AC-7: scale check, render count, canvas grammar · AC-12 and AC-14: latest wins and failure flows, smoke
