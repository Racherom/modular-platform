---
source: spec-0010
character: calm technical tool
tokens: web/src/styles/tokens.css
showcase: /design (dev server only)
---

# Editor design system

This page is the design reference for the editor. Read it before you build a screen, so a new screen looks and behaves like the ones that already ship. It covers the direction, every token, every component, and how the graph canvas draws a stack.

- **Values live in code.** Every token is defined once, in [`src/styles/tokens.css`](src/styles/tokens.css). The tables below copy the values for reading; if they ever differ, the CSS wins.
- **See it live.** Run `pnpm --dir web dev` and open `/design`. It shows every token and component in every state, with a theme switch. The route exists in dev builds only.
- **Decision record.** [Spec 0010](../docs/specs/0010-design-system-graph-editor/index.md) holds the requirements this page meets (AC-1 to AC-7).

## Direction

**Calm technical tool.** Neutral grays, one blue accent, crisp 1px lines, little decoration. The graph and its problems carry the color; the frame around them stays quiet. Density is comfortable, not cramped. Fonts are the system's own. Light and dark are equal: design and check both.

### Build mandate

- Use tokens for every color, size, space, radius, shadow, layer and duration. A unit test (`src/test/color-literals.test.ts`) fails when a CSS file other than `tokens.css`, or a `style` prop in a `.tsx` file, holds a hex, `rgb(`, `rgba(`, `hsl(` or `hsla(` color.
- Style components with CSS Modules (`<Name>.module.css` beside `<Name>.tsx`). Build interactive parts on React Aria Components; style their states through the data attributes they set, such as `[data-hovered]`, `[data-pressed]`, `[data-focus-visible]`, `[data-disabled]`.
- Use Lucide icons, at 16px in controls and 20 to 32px in empty and failure states. Mark decorative icons `aria-hidden="true"`.
- Never show meaning by color alone: pair it with text, an icon, or a shape.
- Every screen has its loading, empty and failure states. Use EmptyState and Failure for them.
- No inline `<style>` or `<script>`: the content policy is `script-src 'self'; style-src 'self'`. Style attributes set from React are fine. React Aria would inject one `<style>` for pressable elements; the `<meta id="react-aria-pressable-style">` in `index.html` stops that, and the rule lives in `base.css`. The browser check's preview server sends the real policy and fails on any blocked style or script.
- Meet WCAG 2.2 AA. The browser check (`pnpm --dir web a11y`) runs axe in both themes on the editor states and on `/design`.

## Themes

The theme follows the OS setting until you choose one. The header's ThemeToggle offers System, Light and Dark and stores the choice in `localStorage` under `mp-theme`.

| Piece | Job |
|---|---|
| `public/theme.js` | A small classic script in `<head>`. Before the first paint it sets `data-theme` on `<html>`, but only when the stored value is exactly `light` or `dark`. |
| `tokens.css` | Light values on `:root`. Dark values under `:root[data-theme="dark"]`, and under `@media (prefers-color-scheme: dark)` for `:root:not([data-theme="light"])`. |
| `src/lib/theme.ts` | Reads and validates the stored choice, resolves System, sets `data-theme`, and gives React Flow the resolved theme. |

When you add a color token, add its dark value in **both** dark blocks.

## Tokens

### Color

| Token | Light | Dark | Use |
|---|---|---|---|
| `--color-bg` | `#ffffff` | `#14161a` | Page background |
| `--color-surface` | `#f6f7f9` | `#1b1e23` | Panels, header, cards |
| `--color-surface-raised` | `#ffffff` | `#22262c` | Nodes, dialogs, popovers |
| `--color-border` | `#d9dde3` | `#343a43` | Dividers, panel edges |
| `--color-border-strong` | `#8a94a3` | `#6b7480` | Input and node borders (3:1 against the surface) |
| `--color-text` | `#1b1f24` | `#e6e9ee` | Body text |
| `--color-text-muted` | `#5b6472` | `#a0a8b5` | Secondary text (still 4.5:1) |
| `--color-accent` | `#2557d6` | `#7ea2ff` | Primary actions, selection, links |
| `--color-accent-hover` | `#1d47b3` | `#9bb8ff` | Hover on accent |
| `--color-on-accent` | `#ffffff` | `#0b1530` | Text on an accent background |
| `--color-focus` | `#2557d6` | `#7ea2ff` | Focus ring |
| `--color-danger` | `#b42318` | `#ff8a80` | Errors, destructive actions |
| `--color-danger-surface` | `#fef3f2` | `#3a1714` | Error backgrounds |
| `--color-warning` | `#9a6700` | `#f5c451` | Warnings |
| `--color-warning-surface` | `#fff8e6` | `#33270a` | Warning backgrounds |
| `--color-success` | `#1a7f37` | `#4ac26b` | Success (StatusBadge) |
| `--color-success-surface` | `#edfaf0` | `#0f2e19` | Success backgrounds |
| `--color-canvas` | `#f6f7f9` | `#14161a` | Canvas background |
| `--color-canvas-dot` | `#c9ced6` | `#2c3139` | Canvas dot grid |
| `--color-edge` | `#6b7480` | `#8b94a1` | Bindings |
| `--color-edge-dimmed` | `#808a99` | `#5f6774` | Dimmed bindings and invalid ports (3:1 against the canvas) |
| `--color-shadow` | `rgb(16 24 40 / 0.12)` | `rgb(0 0 0 / 0.4)` | Shadow color, used by `--shadow-*` only |
| `--color-scrim` | `rgb(16 24 40 / 0.4)` | `rgb(0 0 0 / 0.6)` | Behind a modal dialog |

The values are the spec's starting values; the axe check passed with all of them in both themes, so none changed.

### Type

| Token | Value | Use |
|---|---|---|
| `--font-sans` | `system-ui, -apple-system, "Segoe UI", Roboto, sans-serif` | All interface text |
| `--font-mono` | `ui-monospace, "SF Mono", "Cascadia Mono", Menlo, Consolas, monospace` | Instance, module and capability names, file names, code |
| `--text-xs` | 12px | Badges, notes, captions |
| `--text-sm` | 14px | Controls, panels, tables |
| `--text-md` | 16px | Body text |
| `--text-lg` | 18px | Section headings |
| `--text-xl` | 22px | Screen headings |
| `--text-2xl` | 28px | Page titles, such as `/design` |
| `--leading-body` | 1.5 | Body line height |
| `--leading-heading` | 1.25 | Heading line height |
| `--weight-regular` | 400 | Body |
| `--weight-strong` | 600 | Headings, labels, buttons |

Light and dark use the same type tokens.

### Spacing

A 4px base. Use these for every margin, padding and gap.

| Token | Value |
|---|---|
| `--space-1` | 4px |
| `--space-2` | 8px |
| `--space-3` | 12px |
| `--space-4` | 16px |
| `--space-5` | 20px |
| `--space-6` | 24px |
| `--space-8` | 32px |
| `--space-10` | 40px |
| `--space-12` | 48px |

### Radius, controls and shadow

| Token | Light | Dark | Use |
|---|---|---|---|
| `--radius-sm` | 4px | same | Badges in tables, tooltips, swatches |
| `--radius-md` | 8px | same | Buttons, inputs, nodes, banners |
| `--radius-lg` | 12px | same | Dialogs, cards |
| `--radius-full` | 9999px | same | Pills, dots |
| `--control-md` | 40px | same | Default control height |
| `--control-sm` | 32px | same | Small controls, in toolbars and panels |
| `--shadow-1` | `0 1px 2px` and `0 1px 3px` in `--color-shadow` | same, darker shadow color | Nodes, popovers |
| `--shadow-2` | `0 8px 24px` in `--color-shadow` | same, darker shadow color | Dialogs |

In dark, shadows stay faint and borders do the work.

### Layers, motion, layout and opacity

| Token | Value | Use |
|---|---|---|
| `--layer-panel` | 10 | Panels over the canvas |
| `--layer-popover` | 100 | Popovers, tooltips, select lists |
| `--layer-dialog` | 200 | Dialogs |
| `--duration-fast` | 120ms (0 with reduced motion) | Hover and press |
| `--duration-base` | 200ms (0 with reduced motion) | Panels opening |
| `--ease-standard` | `cubic-bezier(0.2, 0, 0, 1)` | Every transition |
| `--header-height` | 56px | Header |
| `--catalog-width` | 280px | Catalog panel |
| `--inspector-width` | 320px | Inspector panel |
| `--problems-height` | 200px | Problems panel, at most |
| `--opacity-disabled` | 0.5 | Disabled controls only, never to dim text |

All of these are the same in light and dark. Under `prefers-reduced-motion: reduce` every duration is 0.

**Breakpoint.** The layout changes at 1024px. CSS variables cannot drive media queries, so write the literal: `@media (max-width: 1023.98px)`.

## Layout

From 1024px wide the editor shows the header (app name, navigation, file name with the edited marker, toolbars, theme), the catalog panel on the left, the canvas in the middle, the inspector on the right, and the problems panel under the canvas. You can collapse the problems panel. Below 1024px the panels stack under the canvas in this order: problems, inspector, catalog. There is no horizontal page scroll down to 360px. `AppShell` owns this frame.

## Components

All design system components live in [`src/components/ui/`](src/components/ui/). Screen parts (`AppShell`, `GraphScreen`, `GraphView`, `ModuleNode`, `Inspector`, `CatalogPanel`, `ProblemsPanel`) sit one folder up and are built from them.

Every interactive component shows the same focus ring: 2px `--color-focus` with a 2px offset, which has at least 3:1 contrast on every surface.

| Component | Variants and props | States | When to use |
|---|---|---|---|
| **Button** | `variant`: primary, secondary (default), ghost, danger. `size`: md (40px), sm (32px). Text, with an optional 16px icon before it. | default, hover, pressed, focus, disabled, pending text (for example "Deploying…") | Any action. One primary per area, for the main action. Danger for destructive ones (Remove). Ghost for low emphasis actions in toolbars and panel headers. |
| **IconButton** | `label` (required, the accessible name and the tooltip text), `hint` (added to the tooltip, such as a shortcut), `variant`: secondary, ghost (default), danger. `size`: md, sm. | default, hover, pressed, focus, disabled; shows a Tooltip on hover and on focus | An action whose icon is well known (undo, redo, remove). If the icon needs explaining, use a Button with text. |
| **Tooltip** | `content`; wraps one React Aria control | hidden, shown (after 600ms on hover, at once on keyboard focus) | A short hint that adds to a control's name. Never put the only name or vital information in a tooltip. |
| **Select** | `label` (visible, or `hideLabel` for screen readers only), `options`, `value`, `description`, `size`: md, sm | closed, open (list in a popover), with description, focus, disabled; options can be disabled | One choice from a short list, such as "Bound to". |
| **SearchField** | `label`, `hideLabel`, `value`, `placeholder`, `inputRef` | empty, with text (shows a clear button), focus; Escape clears | Filtering a list, such as the catalog. |
| **Badge** | `tone`: neutral (default), info, success, warning, danger | static | A short label beside a name: "optional", "not in catalog", "Edited". The text always says what the tone means. |
| **StatusBadge** | `tone`: neutral, info, success, warning, danger; text label; optional `title` for details | static | The state of something that runs (a module, a deployment). A colored dot plus the state in words. In progress states use info, running uses success, broken states use danger. |
| **Panel** | `title` (also names the region), `actions`, `collapsed`, `bodyId` | expanded, collapsed (heading bar stays) | A named region of a screen: catalog, inspector, problems. Its body scrolls on its own. |
| **Toolbar** | `label` (names the group), `divider` | static; the toolbar is one Tab stop: Tab enters it, arrow keys move focus between its controls, and the next Tab leaves it | A row of related controls, such as History (undo, redo, Tidy layout) and File (open, save, download, deploy). |
| **EmptyState** | `icon`, `title`, `level` (1 to 3 makes the title a heading), body text, `actions` | inline (in a panel), whole screen (`level={1}`) | Anything with nothing to show: no graph open, no catalog match, page not found. Always say how to get out of it. |
| **Failure** | `error`, `onRetry` (same props as before the move to `ui/`) | with Retry, without Retry | A failed call. Leave out `onRetry` when retrying cannot help, such as a 400 from the planner. It is a `role="alert"`, so screen readers hear it at once. |
| **ThemeToggle** | none | System, Light or Dark selected, focus | Once, in the header. `/design` shows a second one to switch the showcase. |
| **Dialog** | `isOpen`, `onClose`, `title`, `role`: dialog or alertdialog, `actions` (last one is the main action) | closed, open | A question that must be answered before you go on, such as "Replace the edited graph?". Focus stays inside, Escape closes it, focus returns to the control that opened it. |

**Managed mode screens.** The deployment list and page, plan review, remove and the signed out view (spec 0006) use these components for their buttons, badges, empty states and failures. Their layout lives in `src/components/Managed.module.css` until #16 rebuilds plan review and remove on Dialog.

## Canvas

The canvas is React Flow, controlled from the graph in the editor store. React Flow only draws and reports gestures; it never changes the graph itself. [`src/styles/reactflow.css`](src/styles/reactflow.css) maps React Flow's `--xy-*` variables to the tokens above.

### Node anatomy

| Part | Value |
|---|---|
| Width | 240px (`NODE_WIDTH`) |
| Header | 56px (`NODE_HEADER`): the instance name in `--font-mono`, then the module name in muted mono, then any badge ("not in catalog") or StatusBadge (managed mode) |
| Port row | 32px (`PORT_ROW`) each, one row per port pair |
| Ports | provides ports on the left, uses ports on the right, each with its capability name as text |
| Handle | 12px (`HANDLE_SIZE`) |
| Surface | `--color-surface-raised`, 1px `--color-border-strong`, `--radius-md`, `--shadow-1` |

The geometry constants live in `src/graph/flow.ts`, because React Flow needs handle positions before it measures. `ModuleNode` writes them as CSS custom properties (`--node-width`, `--node-header`, `--port-row`, `--handle-size`) on its root, so CSS and TypeScript share one source. Nodes cannot be dragged.

### Ports

| Port | Handle |
|---|---|
| Provides (an offer) | Filled |
| Required need | Filled |
| Optional need | Hollow with a dashed border; the inspector also says "optional" in words |
| Valid drop target during a drag | Accent ring |
| Port that does not accept the drop | `--color-edge-dimmed` handle |

A drag can start only at a uses port and end only at a provides port with the same capability name, on another instance whose module is in the catalog.

### Bindings (edges)

| State | Stroke |
|---|---|
| Normal | 1.5px `--color-edge`, smooth step, arrow at the provider |
| Selected, or related to the selected module | 2px `--color-accent` |
| Dimmed (something unrelated is selected) | 1px `--color-edge-dimmed`; the width carries the dimming |
| Keyboard focus | 3px `--color-focus` |

### Selection and dimming

One item is selected at a time, a module or a binding. A selected node gets a 2px `--color-accent` border. Unrelated nodes lose their shadow and their border turns `--color-border`; their text keeps full contrast (dimming never drops text below 4.5:1). An error border stays on a dimmed node, because the problem matters more than the selection.

### Problem marks

| Severity | Mark |
|---|---|
| Error | Lucide `CircleAlert` in `--color-danger`, and a 2px `--color-danger` node border |
| Warning | Lucide `TriangleAlert` in `--color-warning` |

A problem whose path is `modules.<i>.uses.<C>` marks port `C` of node `i`; any other `modules.<i>` path marks the node header. When one place has several problems, the error mark wins, and its accessible name lists every message.

### Canvas chrome

A dot grid in `--color-canvas-dot` on `--color-canvas`, and the zoom in, zoom out and fit controls at the bottom left. Screen readers hear nodes as "whoami, module whoami" and edges as "whoami uses postgresql from postgres".

## Accessibility check

`pnpm --dir web a11y` runs axe with the tags `wcag2a`, `wcag2aa`, `wcag21a`, `wcag21aa` and `wcag22aa` (axe core 4.14 accepts `wcag22aa`), at 1280×800, in light and in dark. It covers the sample graph, the editor with a graph open, during a connect drag, with a module selected, with a binding selected and its select open, with problems shown, the empty editor, `/design`, and `/design` with its Dialog open.

Axe does not measure focus indicators. `src/test/focus-contrast.test.ts` reads `tokens.css` and checks that `--color-focus` has 3:1 against every background a focusable element sits on, in light and in dark. Every ring (outlines, the node outline, the edge stroke) uses that token, so keep it that way.

**Excluded rules: none.** AC-3 allows excluding axe rules that only React Flow's own markup hits. The check passes without any, so nothing is excluded. If you ever need one, list it here with the rule id and the reason.
