import { Search, Unlink } from 'lucide-react'
import type { CatalogEntry, Graph, PlanResult, Problem } from '../api/client'
import { bind, candidates, letPlannerChoose, unbind } from '../graph/edit'
import { markOf, problemInstance } from '../graph/flow'
import { useEditor } from '../lib/store'
import type { GraphEdit } from './GraphView'
import styles from './Inspector.module.css'
import { ProblemMark } from './ModuleNode'
import { Badge } from './ui/Badge'
import { Button } from './ui/Button'
import { Panel } from './ui/Panel'
import { Select, type SelectOption } from './ui/Select'

interface Props {
  graph: Graph
  catalog: CatalogEntry[]
  plan: PlanResult
  /** The plan belongs to exactly this graph (not checking, not failed). */
  planCurrent: boolean
  /** Applies an edit; without it the inspector only shows. */
  onEdit?: (edit: GraphEdit) => void
}

// option ids carry a prefix, so an instance named like a special choice cannot collide with it
const NOT_BOUND = 'none'
const PLANNER = 'planner'
const INVALID = 'invalid'
const instanceOption = (name: string) => `bind:${name}`

/** The selected module or binding, with every edit #5 offers from the keyboard (AC-8, AC-11). */
export function Inspector({ graph, catalog, plan, planCurrent, onEdit }: Props) {
  const selection = useEditor((s) => s.selection)
  const ctx = { graph, catalog, plan, planCurrent, onEdit }
  let body = <p className={styles.muted}>Select a module or a binding to see it here.</p>
  if (selection?.kind === 'module' && Object.hasOwn(graph.modules, selection.instance)) {
    body = <ModuleDetails instance={selection.instance} {...ctx} />
  } else if (selection?.kind === 'binding' && Object.hasOwn(graph.modules, selection.consumer)) {
    body = <BindingDetails consumer={selection.consumer} capability={selection.capability} {...ctx} />
  }
  return <Panel title="Inspector">{body}</Panel>
}

type Ctx = Props

function entryOf(ctx: Ctx, instance: string) {
  const m = ctx.graph.modules[instance]
  return m ? ctx.catalog.find((e) => e.name === m.module) : undefined
}

function ModuleDetails({ instance, ...ctx }: Ctx & { instance: string }) {
  const m = ctx.graph.modules[instance]!
  const entry = entryOf(ctx, instance)
  const problems = ctx.plan.problems.filter((p) => problemInstance(p) === instance)
  const needs = Object.entries(entry?.uses ?? {}).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
  const offers = Object.keys(entry?.provides ?? {}).sort()
  return (
    <div className={styles.details}>
      <header className={styles.header}>
        <h3 className={styles.name}>{instance}</h3>
        <p className={styles.module}>
          module <span className={styles.mono}>{m.module}</span>
        </p>
        {entry ? <p className={styles.description}>{entry.description}</p> : <Badge tone="warning">not in catalog</Badge>}
      </header>

      {problems.length > 0 && <ProblemItems problems={problems} />}

      <section aria-labelledby={`uses-${instance}`} className={styles.section}>
        <h4 id={`uses-${instance}`} className={styles.heading}>
          Uses
        </h4>
        {needs.length === 0 ? (
          <p className={styles.muted}>No needs.</p>
        ) : (
          <ul className={styles.list}>
            {needs.map(([capability, need]) => (
              <li key={capability} className={styles.item}>
                <NeedRow consumer={instance} capability={capability} optional={need.optional ?? false} {...ctx} />
              </li>
            ))}
          </ul>
        )}
      </section>

      <section aria-labelledby={`provides-${instance}`} className={styles.section}>
        <h4 id={`provides-${instance}`} className={styles.heading}>
          Provides
        </h4>
        {offers.length === 0 ? (
          <p className={styles.muted}>No offers.</p>
        ) : (
          <ul className={styles.list}>
            {offers.map((capability) => {
              const users = usersOf(ctx.graph, instance, capability)
              return (
                <li key={capability} className={styles.item}>
                  <span className={styles.mono}>{capability}</span>
                  <span className={styles.muted}>{users.length > 0 ? `Used by ${users.join(', ')}` : 'Not used'}</span>
                </li>
              )
            })}
          </ul>
        )}
      </section>
    </div>
  )
}

function BindingDetails({ consumer, capability, ...ctx }: Ctx & { consumer: string; capability: string }) {
  const provider = ctx.graph.modules[consumer]?.uses?.[capability]
  const optional = entryOf(ctx, consumer)?.uses?.[capability]?.optional ?? false
  const problems = ctx.plan.problems.filter((p) => p.path === `modules.${consumer}.uses.${capability}`)
  return (
    <div className={styles.details}>
      <header className={styles.header}>
        <h3 className={styles.name}>Binding</h3>
        <p className={styles.description}>
          <span className={styles.mono}>{consumer}</span> uses <span className={styles.mono}>{capability}</span> from{' '}
          <span className={styles.mono}>{typeof provider === 'string' ? provider : 'nothing'}</span>
        </p>
      </header>
      {problems.length > 0 && <ProblemItems problems={problems} />}
      <NeedRow consumer={consumer} capability={capability} optional={optional} {...ctx} />
      {ctx.onEdit && (
        <div>
          <Button variant="danger" size="sm" onPress={() => ctx.onEdit?.((g) => unbind(g, consumer, capability))}>
            <Unlink aria-hidden="true" />
            Remove binding
          </Button>
        </div>
      )}
    </div>
  )
}

/** One need: its capability, required or optional, and the "Bound to" select (AC-11). */
function NeedRow({ consumer, capability, optional, ...ctx }: Ctx & { consumer: string; capability: string; optional: boolean }) {
  const uses = ctx.graph.modules[consumer]?.uses
  const has = uses !== undefined && Object.hasOwn(uses, capability)
  const current = has ? uses[capability] : undefined
  const names = candidates(ctx.graph, ctx.catalog, consumer, capability)

  const options: SelectOption[] = []
  let value: string
  if (typeof current === 'string') {
    if (names.includes(current)) value = instanceOption(current)
    else {
      value = INVALID
      options.push({ id: INVALID, label: `${current} (invalid)`, isDisabled: true })
    }
  } else value = current === null ? NOT_BOUND : PLANNER
  options.push(...names.map((n) => ({ id: instanceOption(n), label: n })))
  options.push({ id: NOT_BOUND, label: 'Not bound' }, { id: PLANNER, label: 'Let the planner choose' })

  const auto =
    ctx.planCurrent &&
    typeof current === 'string' &&
    ctx.plan.bindings.some((b) => b.consumer === consumer && b.capability === capability && b.auto)
  const mark = markOf(ctx.plan.problems.filter((p) => p.path === `modules.${consumer}.uses.${capability}`))

  const choose = (id: string) => {
    const onEdit = ctx.onEdit
    if (!onEdit || id === value) return
    if (id === NOT_BOUND) onEdit((g) => unbind(g, consumer, capability))
    else if (id === PLANNER) onEdit((g) => letPlannerChoose(g, consumer, capability))
    else if (id.startsWith('bind:')) {
      const provider = id.slice('bind:'.length)
      onEdit((g) => bind(g, consumer, capability, provider))
    }
  }

  return (
    <div className={styles.need}>
      <div className={styles.needHead}>
        {mark && <ProblemMark mark={mark} />}
        <span className={styles.mono}>{capability}</span>
        <Badge>{optional ? 'optional' : 'required'}</Badge>
      </div>
      <Select
        label={
          <>
            <span className="visually-hidden">{capability} </span>Bound to
          </>
        }
        size="sm"
        options={options}
        value={value}
        onChange={choose}
        isDisabled={!ctx.onEdit}
        description={auto ? 'Bound by the planner' : undefined}
      />
      <div>
        <Button variant="ghost" size="sm" onPress={() => useEditor.getState().findProviders(capability)}>
          <Search aria-hidden="true" />
          Find providers<span className="visually-hidden"> of {capability}</span>
        </Button>
      </div>
    </div>
  )
}

function ProblemItems({ problems }: { problems: Problem[] }) {
  return (
    <ul className={styles.problems} aria-label="Problems of this selection">
      {problems.map((p, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: never reordered, and two problems may share a path and code
        <li key={`${i}|${p.code}`} className={styles.problem}>
          <ProblemMark mark={{ severity: p.severity, messages: [p.message] }} decorative />
          <span>{p.message}</span>
        </li>
      ))}
    </ul>
  )
}

/** The instances whose need for this capability is bound to the given provider. */
function usersOf(graph: Graph, provider: string, capability: string): string[] {
  return Object.keys(graph.modules)
    .filter((j) => {
      const uses = graph.modules[j]?.uses
      return uses !== undefined && Object.hasOwn(uses, capability) && uses[capability] === provider
    })
    .sort()
}
