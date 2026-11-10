import { Search, Trash2, Unlink } from 'lucide-react'
import { type ReactNode, useEffect, useRef, useState } from 'react'
import type { Catalog, CheckResult, Graph, Module, Problem } from '../api/client'
import { acceptRecommendation, bind, candidates, clearConfig, RECOMMENDED, setConfig, unbind } from '../graph/edit'
import { markOf, problemInstance } from '../graph/flow'
import { bindingText, boundProvide, type ConfigField, isExternal, moduleOf, offersOf, settableConfig, slotsOf } from '../graph/model'
import { useEditor } from '../lib/store'
import type { GraphEdit } from './GraphView'
import styles from './Inspector.module.css'
import { ProblemMark } from './ModuleNode'
import { Badge } from './ui/Badge'
import { Button } from './ui/Button'
import { Panel } from './ui/Panel'
import { Select, type SelectOption } from './ui/Select'
import { TextField } from './ui/TextField'

interface Props {
  graph: Graph
  catalog: Catalog
  check: CheckResult
  /** The check result belongs to exactly this graph (not checking, not failed). */
  checkCurrent: boolean
  /** Applies an edit; without it the inspector only shows. */
  onEdit?: (edit: GraphEdit) => void
}

// option ids carry a prefix, so an instance named like a special choice cannot collide with it
const NOT_BOUND = 'none'
const LEFT_OUT = 'left-out'
const INVALID = 'invalid'
const instanceOption = (name: string) => `bind:${name}`

/** The selected module or binding, with every edit #5 offers from the keyboard (AC-8, AC-11). */
export function Inspector({ graph, catalog, check, checkCurrent, onEdit }: Props) {
  const selection = useEditor((s) => s.selection)
  const ctx = { graph, catalog, check, checkCurrent, onEdit }
  let body = <p className={styles.muted}>Select a module or a binding to see it here.</p>
  if (selection?.kind === 'module' && Object.hasOwn(graph.modules, selection.instance)) {
    body = <ModuleDetails instance={selection.instance} {...ctx} />
  } else if (selection?.kind === 'binding' && Object.hasOwn(graph.modules, selection.consumer)) {
    body = <BindingDetails consumer={selection.consumer} slot={selection.slot} {...ctx} />
  }
  return <Panel title="Inspector">{body}</Panel>
}

type Ctx = Props

function ModuleDetails({ instance, ...ctx }: Ctx & { instance: string }) {
  const m = ctx.graph.modules[instance]!
  const external = isExternal(ctx.graph, instance)
  const entry = moduleOf(ctx.graph, ctx.catalog, instance)
  const configPaths = external || !entry ? new Set<string>() : shownConfigPaths(ctx.graph, instance, entry)
  const problems = ctx.check.problems.filter((p) => problemInstance(p) === instance && !configPaths.has(p.path))
  const slots = Object.entries(external ? {} : slotsOf(entry)).sort(([a], [b]) => (a < b ? -1 : a > b ? 1 : 0))
  const offers = offersOf(ctx.graph, ctx.catalog, instance)
  return (
    <div className={styles.details}>
      <header className={styles.header}>
        <h3 className={styles.name}>{instance}</h3>
        {external ? (
          <p className={styles.module}>external service, outside the stack</p>
        ) : (
          <p className={styles.module}>
            module <span className={styles.mono}>{m.module}</span>
          </p>
        )}
        {entry && <p className={styles.description}>{entry.description}</p>}
        {!external && !entry && <Badge tone="warning">not in catalog</Badge>}
      </header>

      {problems.length > 0 && <ProblemItems problems={problems} />}

      {!external && (
        <section aria-labelledby={`uses-${instance}`} className={styles.section}>
          <h4 id={`uses-${instance}`} className={styles.heading}>
            Uses
          </h4>
          {slots.length === 0 ? (
            <p className={styles.muted}>No needs.</p>
          ) : (
            <ul className={styles.list}>
              {slots.map(([slot, s]) => (
                <li key={slot} className={styles.item}>
                  <SlotRow consumer={instance} slot={slot} capability={s.capability} optional={s.optional ?? false} {...ctx} />
                </li>
              ))}
            </ul>
          )}
        </section>
      )}

      {!external && entry && <ConfigSection instance={instance} entry={entry} {...ctx} />}

      <section aria-labelledby={`provides-${instance}`} className={styles.section}>
        <h4 id={`provides-${instance}`} className={styles.heading}>
          Provides
        </h4>
        {offers.length === 0 ? (
          <p className={styles.muted}>No offers.</p>
        ) : (
          <ul className={styles.list}>
            {offers.map((o) => {
              const users = usersOf(ctx, instance, o.provide)
              return (
                <li key={o.provide} className={styles.item}>
                  <span>
                    <span className={styles.mono}>{o.provide}</span>
                    {o.provide !== o.capability && <span className={styles.muted}> {o.capability}</span>}
                    <span className={styles.muted}> {o.version}</span>
                  </span>
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

function BindingDetails({ consumer, slot, ...ctx }: Ctx & { consumer: string; slot: string }) {
  const text = bindingText(ctx.graph.modules[consumer]?.uses?.[slot])
  const spec = slotsOf(moduleOf(ctx.graph, ctx.catalog, consumer))[slot]
  const problems = ctx.check.problems.filter(
    (p) => p.path === `modules.${consumer}.uses.${slot}` || p.path.startsWith(`modules.${consumer}.uses.${slot}.`),
  )
  return (
    <div className={styles.details}>
      <header className={styles.header}>
        <h3 className={styles.name}>Binding</h3>
        <p className={styles.description}>
          <span className={styles.mono}>{consumer}</span> uses <span className={styles.mono}>{slot}</span> from{' '}
          <span className={styles.mono}>{text ?? 'nothing'}</span>
        </p>
      </header>
      {problems.length > 0 && <ProblemItems problems={problems} />}
      <SlotRow consumer={consumer} slot={slot} capability={spec?.capability ?? ''} optional={spec?.optional ?? false} {...ctx} />
      {ctx.onEdit && (
        <div>
          <Button variant="danger" size="sm" onPress={() => ctx.onEdit?.((g) => unbind(g, consumer, slot))}>
            <Unlink aria-hidden="true" />
            Remove binding
          </Button>
        </div>
      )}
    </div>
  )
}

/** One slot: its name and capability, required or optional, and the "Bound to" select (AC-11). */
function SlotRow({
  consumer,
  slot,
  capability,
  optional,
  ...ctx
}: Ctx & { consumer: string; slot: string; capability: string; optional: boolean }) {
  const uses = ctx.graph.modules[consumer]?.uses
  const has = uses !== undefined && Object.hasOwn(uses, slot)
  const current = has ? uses[slot] : undefined
  const text = bindingText(current)
  const names = candidates(ctx.graph, ctx.catalog, consumer, slot)

  const options: SelectOption[] = []
  let value: string
  if (text !== undefined) {
    if (names.includes(text)) value = instanceOption(text)
    else {
      value = INVALID
      options.push({ id: INVALID, label: `${text} (invalid)`, isDisabled: true })
    }
  } else if (current === null) value = NOT_BOUND
  else {
    // the graph leaves the use out: nothing is bound until you pick or accept a provider
    value = LEFT_OUT
    options.push({ id: LEFT_OUT, label: 'Choose a provider', isDisabled: true })
  }
  options.push(...names.map((n) => ({ id: instanceOption(n), label: n })))
  options.push({ id: NOT_BOUND, label: 'Not bound' })

  const atUse = ctx.check.problems.filter((p) => p.path === `modules.${consumer}.uses.${slot}`)
  const mark = markOf(atUse)
  const recommended = ctx.checkCurrent && !has ? recommendationOf(atUse) : undefined

  // after Accept the button goes away, so focus moves to the binding field, which now shows the provider
  const field = useRef<HTMLDivElement>(null)
  const focusField = useRef(false)
  useEffect(() => {
    if (!focusField.current || text === undefined) return
    focusField.current = false
    field.current?.querySelector<HTMLElement>('button')?.focus()
  }, [text])
  const accept = (candidate: string) => {
    focusField.current = true
    ctx.onEdit?.((g) => acceptRecommendation(g, consumer, slot, candidate))
  }

  const choose = (id: string) => {
    const onEdit = ctx.onEdit
    if (!onEdit || id === value) return
    if (id === NOT_BOUND) onEdit((g) => unbind(g, consumer, slot))
    else if (id.startsWith('bind:')) {
      const provider = id.slice('bind:'.length)
      onEdit((g) => bind(g, consumer, slot, provider))
    }
  }

  return (
    <div className={styles.need}>
      <div className={styles.needHead}>
        {mark && <ProblemMark mark={mark} />}
        <span className={styles.mono}>{slot}</span>
        {slot !== capability && <span className={styles.muted}>{capability}</span>}
        <Badge>{optional ? 'optional' : 'required'}</Badge>
      </div>
      <div ref={field}>
        <Select
          label={
            <>
              <span className="visually-hidden">{slot} </span>Bound to
            </>
          }
          size="sm"
          options={options}
          value={value}
          onChange={choose}
          isDisabled={!ctx.onEdit}
          description={recommended ? `Recommended: ${recommended}` : undefined}
        />
      </div>
      {recommended && ctx.onEdit && (
        <div>
          <Button size="sm" onPress={() => accept(recommended)} aria-label={`Accept ${recommended} for ${slot}`}>
            Accept
          </Button>
        </div>
      )}
      <div>
        <Button variant="ghost" size="sm" onPress={() => useEditor.getState().findProviders(capability)}>
          <Search aria-hidden="true" />
          Find providers<span className="visually-hidden"> of {capability}</span>
        </Button>
      </div>
    </div>
  )
}

/**
 * The provider the checks recommend at one use (spec 0028): the candidate of `binding_recommended`,
 * or of `optional_unbound` when it has exactly one.
 */
function recommendationOf(problems: readonly Problem[]): string | undefined {
  const p = problems.find((p) => (p.code === RECOMMENDED || p.code === 'optional_unbound') && p.candidates?.length === 1)
  return p?.candidates?.[0]
}

const configPath = (instance: string, value: string) => `modules.${instance}.config.${value}`

/** The config paths the Config section shows: each value the graph may set, and each key it holds. */
function shownConfigPaths(graph: Graph, instance: string, entry: Module): Set<string> {
  const names = [...settableConfig(entry).map((f) => f.name), ...Object.keys(graph.modules[instance]?.config ?? {})]
  return new Set(names.map((v) => configPath(instance, v)))
}

/**
 * The config values of a module instance that the graph may set, each with its problems
 * under it, and the keys the graph holds that the entry does not offer, each with a remove
 * button (spec 0025, AC-29). Values are checked only by the checks.
 */
function ConfigSection({ instance, entry, ...ctx }: Ctx & { instance: string; entry: Module }) {
  const set = ctx.graph.modules[instance]?.config ?? {}
  const fields = settableConfig(entry)
  const settable = new Set(fields.map((f) => f.name))
  const unknown = Object.keys(set)
    .filter((k) => !settable.has(k))
    .sort()
  const problemsAt = (v: string) => ctx.check.problems.filter((p) => p.path === configPath(instance, v))
  return (
    <section aria-labelledby={`config-${instance}`} className={styles.section}>
      <h4 id={`config-${instance}`} className={styles.heading}>
        Config
      </h4>
      {fields.length === 0 ? (
        <p className={styles.muted}>No values to set.</p>
      ) : (
        <ul className={styles.list}>
          {fields.map((f) => (
            <li key={f.name} className={styles.item}>
              <ConfigRow
                instance={instance}
                field={f}
                text={Object.hasOwn(set, f.name) ? set[f.name] : undefined}
                problems={problemsAt(f.name)}
                onEdit={ctx.onEdit}
              />
            </li>
          ))}
        </ul>
      )}
      {unknown.length > 0 && (
        <div className={styles.section}>
          <h5 className={styles.heading}>Unknown values</h5>
          <ul className={styles.list} aria-label="Unknown values">
            {unknown.map((k) => (
              <li key={k} className={styles.need}>
                <span>
                  <span className={styles.mono}>{k}</span> <span className={styles.muted}>{set[k]}</span>
                </span>
                {problemsAt(k).length > 0 && <ProblemItems problems={problemsAt(k)} />}
                {ctx.onEdit && (
                  <div>
                    <Button variant="ghost" size="sm" onPress={() => ctx.onEdit?.((g) => clearConfig(g, instance, k))}>
                      <Trash2 aria-hidden="true" />
                      Remove <span className="visually-hidden">{k}</span>
                    </Button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  )
}

const UNSET = 'unset'
const choiceOption = (v: string) => `value:${v}`

/** One config value: a select for enum and bool, a text field otherwise (AC-29). */
function ConfigRow({
  instance,
  field,
  text,
  problems,
  onEdit,
}: {
  instance: string
  field: ConfigField
  /** The graph's text, or undefined when the graph leaves the value out. */
  text: string | undefined
  problems: Problem[]
  onEdit: Props['onEdit']
}) {
  const { name, value: v } = field
  const mark = markOf(problems)
  const hint = `${v.description} ${v.default !== undefined ? `Default ${v.default}.` : v.optional ? 'No default, optional.' : 'No default.'}`
  const write = (next: string | undefined) => {
    if (!onEdit || next === text) return
    onEdit((g) => (next === undefined ? clearConfig(g, instance, name) : setConfig(g, instance, name, next)))
  }
  const choices = v.type === 'enum' ? (v.values ?? []) : v.type === 'bool' ? ['true', 'false'] : undefined
  let control: ReactNode
  if (choices) {
    const options: SelectOption[] = [{ id: UNSET, label: v.default !== undefined ? `Default (${v.default})` : 'Not set' }]
    if (text !== undefined && !choices.includes(text))
      options.push({ id: choiceOption(text), label: `${text} (invalid)`, isDisabled: true })
    options.push(...choices.map((c) => ({ id: choiceOption(c), label: c })))
    control = (
      <Select
        label={name}
        size="sm"
        options={options}
        value={text === undefined ? UNSET : choiceOption(text)}
        onChange={(id) => write(id === UNSET ? undefined : id.slice('value:'.length))}
        isDisabled={!onEdit}
        description={hint}
      />
    )
  } else {
    // a remount on a changed graph text drops the draft, so undo shows the restored value
    control = (
      <ConfigText
        key={text === undefined ? 'unset' : `set:${text}`}
        name={name}
        text={text}
        hint={hint}
        placeholder={v.default}
        onWrite={write}
        isDisabled={!onEdit}
        isInvalid={problems.some((p) => p.severity === 'error')}
      />
    )
  }
  return (
    <div className={styles.need}>
      <div className={styles.needHead}>
        {mark && <ProblemMark mark={mark} />}
        <Badge>{v.type}</Badge>
        {v.optional && <Badge>optional</Badge>}
      </div>
      {control}
      {problems.length > 0 && <ProblemItems problems={problems} />}
    </div>
  )
}

/** A text config value: typed into a draft, written on Enter or blur; clearing it removes the key. */
function ConfigText({
  name,
  text,
  hint,
  placeholder,
  onWrite,
  isDisabled,
  isInvalid,
}: {
  name: string
  text: string | undefined
  hint: string
  placeholder: string | undefined
  onWrite: (next: string | undefined) => void
  isDisabled: boolean
  isInvalid: boolean
}) {
  const [draft, setDraft] = useState(text ?? '')
  return (
    <TextField
      label={name}
      size="sm"
      value={draft}
      onChange={setDraft}
      onCommit={(t) => onWrite(t === '' ? undefined : t)}
      description={hint}
      placeholder={placeholder}
      isDisabled={isDisabled}
      isInvalid={isInvalid}
    />
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

/** The consumers, as instance.slot, whose binding reaches the given provide. */
function usersOf(ctx: Ctx, provider: string, provide: string): string[] {
  const out: string[] = []
  for (const j of Object.keys(ctx.graph.modules).sort()) {
    for (const slot of Object.keys(ctx.graph.modules[j]?.uses ?? {}).sort()) {
      const b = boundProvide(ctx.graph, ctx.catalog, j, slot)
      if (b?.provider === provider && b.provide === provide) out.push(slot === provide ? j : `${j}.${slot}`)
    }
  }
  return out
}
