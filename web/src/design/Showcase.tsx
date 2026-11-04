import { Inbox, Plus, Redo2, Trash2, Undo2 } from 'lucide-react'
import { type ReactNode, useEffect, useId, useState } from 'react'
import type { CatalogEntry, Graph, Problem } from '../api/client'
import { GraphView } from '../components/GraphView'
import { Badge, type BadgeTone } from '../components/ui/Badge'
import { Button, type ButtonVariant } from '../components/ui/Button'
import buttonStyles from '../components/ui/Button.module.css'
import { Dialog } from '../components/ui/Dialog'
import { EmptyState } from '../components/ui/EmptyState'
import { Failure } from '../components/ui/Failure'
import { IconButton } from '../components/ui/IconButton'
import iconButtonStyles from '../components/ui/IconButton.module.css'
import { Panel } from '../components/ui/Panel'
import { SearchField } from '../components/ui/SearchField'
import { Select } from '../components/ui/Select'
import { StatusBadge, type StatusTone } from '../components/ui/StatusBadge'
import { ThemeToggle } from '../components/ui/ThemeToggle'
import { Toolbar } from '../components/ui/Toolbar'
import tooltipStyles from '../components/ui/Tooltip.module.css'
import { useEditor } from '../lib/store'
import { useResolvedTheme } from '../lib/theme'
import styles from './Showcase.module.css'

/*
 * The design system showcase (spec 0010, AC-5): every token and every component in every state
 * web/design.md lists, in the current theme. Dev builds only; routes/design.tsx loads it.
 * Hover, pressed and focus cannot be held still on a live control, so those states show as
 * static specimens: the component's own class and the data attribute React Aria sets.
 */

const colors = [
  ['--color-bg', 'Page'],
  ['--color-surface', 'Panels, header'],
  ['--color-surface-raised', 'Nodes, dialogs, popovers'],
  ['--color-border', 'Dividers, panel edges'],
  ['--color-border-strong', 'Input and node borders'],
  ['--color-text', 'Body text'],
  ['--color-text-muted', 'Secondary text'],
  ['--color-accent', 'Primary actions, selection, links'],
  ['--color-accent-hover', 'Hover on accent'],
  ['--color-on-accent', 'Text on accent'],
  ['--color-focus', 'Focus ring'],
  ['--color-danger', 'Errors'],
  ['--color-danger-surface', 'Error backgrounds'],
  ['--color-warning', 'Warnings'],
  ['--color-warning-surface', 'Warning backgrounds'],
  ['--color-success', 'Success'],
  ['--color-success-surface', 'Success backgrounds'],
  ['--color-canvas', 'Canvas background'],
  ['--color-canvas-dot', 'Canvas grid'],
  ['--color-edge', 'Bindings'],
  ['--color-edge-dimmed', 'Dimmed bindings, invalid ports'],
  ['--color-shadow', 'Shadow color'],
  ['--color-scrim', 'Behind a dialog'],
] as const

const textSizes = ['--text-xs', '--text-sm', '--text-md', '--text-lg', '--text-xl', '--text-2xl'] as const
const spaces = [
  '--space-1',
  '--space-2',
  '--space-3',
  '--space-4',
  '--space-5',
  '--space-6',
  '--space-8',
  '--space-10',
  '--space-12',
] as const
const radii = ['--radius-sm', '--radius-md', '--radius-lg', '--radius-full'] as const
const shadows = ['--shadow-1', '--shadow-2'] as const
const others = [
  ['--font-sans', 'Interface text'],
  ['--font-mono', 'Instance, module and capability names'],
  ['--leading-body', 'Line height of body text'],
  ['--leading-heading', 'Line height of headings'],
  ['--weight-regular', 'Body weight'],
  ['--weight-strong', 'Headings, labels, buttons'],
  ['--control-md', 'Control height'],
  ['--control-sm', 'Small control height'],
  ['--layer-panel', 'Panels'],
  ['--layer-popover', 'Popovers and tooltips'],
  ['--layer-dialog', 'Dialogs'],
  ['--duration-fast', 'Hover and press'],
  ['--duration-base', 'Panels opening'],
  ['--ease-standard', 'Every transition'],
  ['--header-height', 'Header'],
  ['--catalog-width', 'Catalog panel'],
  ['--inspector-width', 'Inspector panel'],
  ['--problems-height', 'Problems panel, at most'],
  ['--opacity-disabled', 'Disabled controls only'],
] as const

const variants: ButtonVariant[] = ['primary', 'secondary', 'ghost', 'danger']
const tones: BadgeTone[] = ['neutral', 'info', 'success', 'warning', 'danger']
const stateTones: [StatusTone, string][] = [
  ['neutral', 'removed'],
  ['info', 'applying'],
  ['success', 'running'],
  ['warning', 'rolled back'],
  ['danger', 'failed'],
]

/** Reads the current value of every token, again whenever the theme changes. */
function useTokenValues(names: readonly string[]): Record<string, string> {
  const theme = useResolvedTheme()
  const [values, setValues] = useState<Record<string, string>>({})
  // biome-ignore lint/correctness/useExhaustiveDependencies: the theme changes the computed values
  useEffect(() => {
    const style = getComputedStyle(document.documentElement)
    setValues(Object.fromEntries(names.map((n) => [n, style.getPropertyValue(n).trim()])))
  }, [names, theme])
  return values
}

const allTokens = [...colors.map(([n]) => n), ...textSizes, ...spaces, ...radii, ...shadows, ...others.map(([n]) => n)]

/** The showcase page. */
export function Showcase() {
  const values = useTokenValues(allTokens)
  const theme = useResolvedTheme()
  return (
    <div className={styles.page} data-showcase="mp-design-showcase">
      <header className={styles.intro}>
        <div>
          <h1>Design system</h1>
          <p>
            Every token and component of the editor, in the {theme} theme. The reference is <code>web/design.md</code>; the values live in{' '}
            <code>web/src/styles/tokens.css</code>.
          </p>
        </div>
        <div className={styles.themeControl}>
          <span className={styles.rowLabel}>ThemeToggle</span>
          <ThemeToggle />
        </div>
      </header>

      <nav aria-label="Showcase sections" className={styles.toc}>
        <a href="#tokens">Tokens</a>
        <a href="#components">Components</a>
        <a href="#canvas">Canvas</a>
      </nav>

      <h2 id="tokens" className={styles.part}>
        Tokens
      </h2>

      <Section title="Color">
        <ul className={styles.swatches}>
          {colors.map(([name, use]) => (
            <li key={name} className={styles.swatch}>
              <span className={styles.chip} style={{ background: `var(${name})` }} />
              <code>{name}</code>
              <span className={styles.value}>{values[name]}</span>
              <span className={styles.use}>{use}</span>
            </li>
          ))}
        </ul>
      </Section>

      <Section title="Type">
        <ul className={styles.stack}>
          {textSizes.map((name) => (
            <li key={name} className={styles.typeRow}>
              <code>
                {name} · {values[name]}
              </code>
              <span style={{ fontSize: `var(${name})` }}>Wire a module's need to another module's offer</span>
            </li>
          ))}
          <li className={styles.typeRow}>
            <code>--font-mono</code>
            <span className={styles.mono}>whoami uses postgresql from postgres</span>
          </li>
        </ul>
      </Section>

      <Section title="Spacing">
        <ul className={styles.stack}>
          {spaces.map((name) => (
            <li key={name} className={styles.spaceRow}>
              <code>
                {name} · {values[name]}
              </code>
              <span className={styles.bar} style={{ width: `var(${name})` }} />
            </li>
          ))}
        </ul>
      </Section>

      <Section title="Radius and shadow">
        <ul className={styles.boxes}>
          {radii.map((name) => (
            <li key={name}>
              <span className={styles.box} style={{ borderRadius: `var(${name})` }} />
              <code>{name}</code>
              <span className={styles.value}>{values[name]}</span>
            </li>
          ))}
          {shadows.map((name) => (
            <li key={name}>
              <span className={styles.box} style={{ boxShadow: `var(${name})` }} />
              <code>{name}</code>
            </li>
          ))}
        </ul>
      </Section>

      <Section title="Fonts, controls, layers, motion, layout and opacity">
        <table className={styles.table}>
          <thead>
            <tr>
              <th scope="col">Token</th>
              <th scope="col">Value</th>
              <th scope="col">Use</th>
            </tr>
          </thead>
          <tbody>
            {others.map(([name, use]) => (
              <tr key={name}>
                <td>
                  <code>{name}</code>
                </td>
                <td className={styles.value}>{values[name]}</td>
                <td>{use}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </Section>

      <h2 id="components" className={styles.part}>
        Components
      </h2>

      <Section title="Button">
        {(['md', 'sm'] as const).map((size) => (
          <div key={size} className={styles.matrix}>
            <span className={styles.rowLabel}>{size}</span>
            {variants.map((variant) => (
              <Button key={variant} variant={variant} size={size}>
                {variant === 'primary' && <Plus aria-hidden="true" />}
                {label(variant)}
              </Button>
            ))}
          </div>
        ))}
        <States>
          {variants.map((variant) => (
            <div key={variant} className={styles.matrix}>
              <span className={styles.rowLabel}>{variant}</span>
              <Specimen state="hover">
                <span className={buttonStyles.button} data-variant={variant} data-size="md" data-hovered>
                  {label(variant)}
                </span>
              </Specimen>
              <Specimen state="focus">
                <span className={buttonStyles.button} data-variant={variant} data-size="md" data-focus-visible>
                  {label(variant)}
                </span>
              </Specimen>
              <Specimen state="disabled">
                <Button variant={variant} isDisabled>
                  {label(variant)}
                </Button>
              </Specimen>
            </div>
          ))}
        </States>
      </Section>

      <Section title="IconButton and Tooltip">
        <div className={styles.matrix}>
          <IconButton label="Undo" hint="Ctrl+Z">
            <Undo2 aria-hidden="true" />
          </IconButton>
          <IconButton label="Redo" hint="Ctrl+Shift+Z" variant="secondary">
            <Redo2 aria-hidden="true" />
          </IconButton>
          <IconButton label="Remove" variant="danger">
            <Trash2 aria-hidden="true" />
          </IconButton>
          <IconButton label="Undo, small" size="sm">
            <Undo2 aria-hidden="true" />
          </IconButton>
          <IconButton label="Nothing to undo" isDisabled>
            <Undo2 aria-hidden="true" />
          </IconButton>
        </div>
        <States>
          <div className={styles.matrix}>
            <Specimen state="hover">
              <span className={iconButtonStyles.button} data-variant="ghost" data-size="md" data-hovered>
                <Undo2 aria-hidden="true" />
              </span>
            </Specimen>
            <Specimen state="focus">
              <span className={iconButtonStyles.button} data-variant="ghost" data-size="md" data-focus-visible>
                <Undo2 aria-hidden="true" />
              </span>
            </Specimen>
            <Specimen state="tooltip">
              <span className={`${tooltipStyles.tooltip} ${styles.staticTooltip}`}>Undo (Ctrl+Z)</span>
            </Specimen>
          </div>
        </States>
      </Section>

      <Section title="Select">
        <SelectDemo />
      </Section>

      <Section title="SearchField">
        <SearchDemo />
      </Section>

      <Section title="Badge and StatusBadge">
        <div className={styles.matrix}>
          <span className={styles.rowLabel}>Badge</span>
          {tones.map((tone) => (
            <Badge key={tone} tone={tone}>
              {tone}
            </Badge>
          ))}
        </div>
        <div className={styles.matrix}>
          <span className={styles.rowLabel}>StatusBadge</span>
          {stateTones.map(([tone, text]) => (
            <StatusBadge key={tone} tone={tone}>
              {text}
            </StatusBadge>
          ))}
        </div>
      </Section>

      <Section title="Panel and Toolbar">
        <PanelDemo />
      </Section>

      <Section title="EmptyState">
        <div className={styles.columns}>
          <div className={styles.frame}>
            <EmptyState icon={Inbox} title="No modules match">
              <p>Try another word, or clear the filter.</p>
            </EmptyState>
          </div>
          <div className={styles.frame}>
            <EmptyState icon={Inbox} level={3} title="Nothing deployed yet" actions={<Button size="sm">Open the editor</Button>}>
              <p>Open a graph and choose Deploy.</p>
            </EmptyState>
          </div>
        </div>
      </Section>

      <Section title="Failure">
        <div className={styles.stack}>
          <Failure error={new Error('The planner is busy. Try again in a minute.')} onRetry={() => undefined} />
          <Failure error={new Error('The graph has a module without a name.')} />
        </div>
      </Section>

      <Section title="Dialog">
        <DialogDemo />
      </Section>

      <h2 id="canvas" className={styles.part}>
        Canvas
      </h2>
      <Section title="Nodes, ports, bindings and problem marks">
        <p className={styles.note}>
          A node shows its instance and module name, provides ports on the left and uses ports on the right. A filled handle is a required
          need, a hollow dashed one is optional. Click a node or a binding to see the selection and the dimming.
        </p>
        <CanvasDemo />
      </Section>
    </div>
  )
}

function label(variant: ButtonVariant): string {
  return variant === 'primary' ? 'Deploy' : variant === 'danger' ? 'Remove' : variant === 'ghost' ? 'Tidy layout' : 'Save as file'
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  const id = useId()
  return (
    <section className={styles.section} aria-labelledby={id}>
      <h3 id={id} className={styles.title}>
        {title}
      </h3>
      {children}
    </section>
  )
}

/** Static specimens of states a live control cannot hold. */
function States({ children }: { children: ReactNode }) {
  return (
    <div className={styles.states}>
      <p className={styles.note}>States (static specimens)</p>
      {children}
    </div>
  )
}

function Specimen({ state, children }: { state: string; children: ReactNode }) {
  return (
    <figure className={styles.specimen}>
      <div aria-hidden="true">{children}</div>
      <figcaption>{state}</figcaption>
    </figure>
  )
}

function SelectDemo() {
  const options = [
    { id: 'postgres', label: 'postgres' },
    { id: 'postgres-2', label: 'postgres-2' },
    { id: 'none', label: 'Not bound' },
    { id: 'auto', label: 'Let the planner choose' },
  ]
  const [value, setValue] = useState<string | null>('postgres')
  return (
    <div className={styles.columns}>
      <Select label="Bound to" options={options} value={value} onChange={setValue} />
      <Select
        label="Bound to, with a note"
        options={options}
        value="postgres"
        onChange={() => undefined}
        description="bound by the planner"
      />
      <Select label="Bound to, small" size="sm" options={options} value="none" onChange={() => undefined} />
      <Select label="Bound to, disabled" options={options} value="auto" onChange={() => undefined} isDisabled />
    </div>
  )
}

function SearchDemo() {
  const [empty, setEmpty] = useState('')
  const [filled, setFilled] = useState('postgresql')
  return (
    <div className={styles.columns}>
      <SearchField label="Filter the catalog" value={empty} onChange={setEmpty} placeholder="Name or capability" />
      <SearchField label="Filter, with text" value={filled} onChange={setFilled} />
      <SearchField label="Filter, label hidden" hideLabel value="" onChange={() => undefined} placeholder="Label hidden" />
    </div>
  )
}

function PanelDemo() {
  const [collapsed, setCollapsed] = useState(false)
  const bodyId = useId()
  return (
    <div className={styles.columns}>
      <div className={styles.frame}>
        <Panel
          title="Problems"
          bodyId={bodyId}
          collapsed={collapsed}
          actions={
            <Button variant="ghost" size="sm" aria-expanded={!collapsed} aria-controls={bodyId} onPress={() => setCollapsed((c) => !c)}>
              {collapsed ? 'Show' : 'Hide'}
            </Button>
          }
        >
          <p className={styles.pad}>The body scrolls on its own; the heading bar stays.</p>
        </Panel>
      </div>
      <div className={styles.frame}>
        <div className={styles.pad}>
          <Toolbar label="History, example" divider>
            <IconButton label="Undo, example" size="sm">
              <Undo2 aria-hidden="true" />
            </IconButton>
            <IconButton label="Redo, example" size="sm">
              <Redo2 aria-hidden="true" />
            </IconButton>
            <Button variant="ghost" size="sm">
              Tidy layout
            </Button>
          </Toolbar>
          <p className={styles.note}>Arrow keys move between the controls of a toolbar.</p>
        </div>
      </div>
    </div>
  )
}

function DialogDemo() {
  const [open, setOpen] = useState(false)
  return (
    <div className={styles.matrix}>
      <span className={styles.rowLabel}>Dialog</span>
      <Button onPress={() => setOpen(true)}>Open the dialog</Button>
      <Dialog
        isOpen={open}
        onClose={() => setOpen(false)}
        title="Replace the edited graph?"
        role="alertdialog"
        actions={
          <>
            <Button onPress={() => setOpen(false)}>Cancel</Button>
            <Button onPress={() => setOpen(false)}>Replace</Button>
            <Button variant="primary" onPress={() => setOpen(false)}>
              Save as file
            </Button>
          </>
        }
      >
        <p>Your working graph has changes that are not saved as a file. Focus stays in here; Escape closes it.</p>
      </Dialog>
    </div>
  )
}

const canvasCatalog = [
  {
    apiVersion: 'modular-platform/v0alpha1',
    kind: 'Module',
    name: 'postgres',
    description: 'A database.',
    image: 'postgres:18.6',
    provides: { postgresql: { port: 5432 } },
  },
  {
    apiVersion: 'modular-platform/v0alpha1',
    kind: 'Module',
    name: 'app',
    description: 'An app with a required and an optional need.',
    image: 'example.test/app:1',
    provides: { http: { port: 8080 } },
    uses: { postgresql: {}, cache: { optional: true } },
  },
] as unknown as CatalogEntry[]

const canvasGraph: Graph = {
  apiVersion: 'modular-platform/v0alpha1',
  kind: 'Graph',
  name: 'showcase',
  modules: {
    db: { module: 'postgres' },
    shop: { module: 'app', uses: { postgresql: 'db' } },
    broken: { module: 'app', uses: { postgresql: null } },
    legacy: { module: 'old-thing' },
  },
}

const canvasProblems: Problem[] = [
  {
    severity: 'error',
    code: 'unbound_required',
    path: 'modules.broken.uses.postgresql',
    message: 'broken needs postgresql, and it is not bound.',
  },
  { severity: 'warning', code: 'unknown_module', path: 'modules.legacy', message: 'legacy uses old-thing, which is not in the catalog.' },
]

function CanvasDemo() {
  // the canvas writes its selection to the editor store; leave none behind for the editor
  useEffect(() => () => useEditor.getState().select(null), [])
  return (
    <div className={styles.canvas}>
      <GraphView graph={canvasGraph} catalog={canvasCatalog} problems={canvasProblems} />
    </div>
  )
}
