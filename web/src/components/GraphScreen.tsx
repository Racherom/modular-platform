import { useMutation, useQuery } from '@tanstack/react-query'
import { ReactFlowProvider } from '@xyflow/react'
import { Check, Download, Info, LayoutGrid, Redo2, Rocket, Save, Undo2 } from 'lucide-react'
import { type ReactNode, useEffect, useId, useRef, useState } from 'react'
import { type Catalog, type CheckResult, fetchCatalog, type Graph, type RendererError, renderGraph } from '../api/client'
import { acceptAllRecommendations, RECOMMENDED } from '../graph/edit'
import type { Positions } from '../graph/flow'
import { mode } from '../lib/config'
import { download, saveGraphFile } from '../lib/download'
import { historyKey, historyShortcut, isApple, isFormControl } from '../lib/keys'
import { type CheckState, useEditor } from '../lib/store'
import { HeaderContent, Workspace } from './AppShell'
import { CatalogPanel } from './CatalogPanel'
import styles from './GraphScreen.module.css'
import { type GraphEdit, GraphView } from './GraphView'
import { Inspector } from './Inspector'
import { ProblemsPanel } from './ProblemsPanel'
import { ReviewDialog } from './ReviewDialog'
import { Badge } from './ui/Badge'
import { Button } from './ui/Button'
import { Failure } from './ui/Failure'
import { IconButton } from './ui/IconButton'
import { Toolbar } from './ui/Toolbar'

/** The catalog, fetched once per page. */
export function useCatalog() {
  return useQuery({ queryKey: ['catalog'], queryFn: fetchCatalog })
}

/** Undo, redo and Tidy layout for the working graph (AC-15, AC-16). */
export interface HistoryControls {
  canUndo: boolean
  canRedo: boolean
  onUndo: () => void
  onRedo: () => void
  onTidy: () => void
}

/** What an editable screen adds: the working copy's state and its edit actions. */
export interface Editing {
  positions: Positions
  edited: boolean
  checkState: CheckState
  checkError: RendererError | null
  onRetry: () => void
  /** Applies a user edit to the working graph. */
  onEdit: (edit: GraphEdit) => void
  /** Called after Save as file. */
  onSaved: () => void
  /** Undo, redo and Tidy layout; without them the buttons show disabled (the sample). */
  history?: HistoryControls
  /** Adds one instance of a catalog module, placed with the catalog's node sizes (spec 0029); the managed stack view has none. */
  onAdd?: (module: string, catalog: Catalog) => void
  /** False while the graph is the empty start graph: there is nothing to save yet (AC-9). */
  canSave?: boolean
  /** Saving the working copy failed, so changes are not kept after a reload (AC-17). */
  storageFailed?: boolean
}

interface Props {
  graph: Graph
  check: CheckResult
  fileName: string
  toolbar?: ReactNode
  /** Makes the canvas editable; without it the graph is only shown. */
  editing?: Editing
  /** A hint above the canvas, such as the sample's "changes make a copy". */
  notice?: ReactNode
  /** A message over the empty canvas, such as "No graph open" (spec 0029, AC-9). */
  overlay?: ReactNode
}

/**
 * Why Download zip (and Deploy) is disabled, or null when it is allowed (AC-14): only a current
 * check without errors allows it; warnings never block. An open recommendation is an error too,
 * with its own reason (spec 0028). A graph with no module has the first reason (spec 0029, AC-10).
 */
export function downloadBlocked(checkState: CheckState, check: CheckResult, modules = 1): string | null {
  if (modules === 0) return 'Add a module first'
  if (checkState === 'checking') return 'Checking the graph'
  if (checkState === 'failed') return 'Not checked, retry first'
  if (check.ok) return null
  const recommended = check.problems.filter((p) => p.code === RECOMMENDED).length
  const errors = check.problems.filter((p) => p.severity === 'error').length - recommended
  const accept = recommended > 0 ? `Accept or change ${recommended} recommended ${recommended === 1 ? 'binding' : 'bindings'} first` : null
  const fix = errors > 0 ? `Fix ${errors} ${errors === 1 ? 'error' : 'errors'} to download` : null
  if (fix && accept) return `${fix}. ${accept}`
  return fix ?? accept ?? 'Fix the errors to download'
}

/** A checked graph in the editor: header toolbar, catalog, canvas, inspector and problems. */
export function GraphScreen({ graph, check, fileName, toolbar, editing, notice, overlay }: Props) {
  const catalog = useCatalog()
  // a render that finds an error returns a newer check to show, but only for the check result it came from
  const [rendered, setRendered] = useState<{ from: CheckResult; check: CheckResult }>()
  const shownResult = rendered?.from === check ? rendered.check : check
  const [reviewing, setReviewing] = useState(false)
  const [skipped, setSkipped] = useState(0)
  const fileToolbar = useRef<HTMLDivElement>(null)
  const announcement = useEditor((s) => s.announcement)
  const reasonId = useId()
  const zip = useMutation({
    mutationFn: () => renderGraph(graph),
    onSuccess: (result) => {
      if ('zip' in result) download(result.zip, `${graph.name}.zip`)
      else setRendered({ from: check, check: result.check })
    },
  })

  if (catalog.isError) return <Failure error={catalog.error} onRetry={() => void catalog.refetch()} />
  if (!catalog.data) return <p>Loading the catalog…</p>

  const checkState = editing?.checkState ?? 'current'
  const blocked = downloadBlocked(checkState, shownResult, Object.keys(graph.modules).length)
  // a stale check shows no Accept, so you never accept against an old graph (spec 0028)
  const recommendations = checkState === 'current' ? shownResult.problems.filter((p) => p.code === RECOMMENDED) : []
  const acceptAll = () => {
    if (!editing) return
    let skipped = 0
    editing.onEdit((g) => {
      const accepted = acceptAllRecommendations(g, recommendations)
      skipped = accepted.skipped
      return accepted.graph
    })
    setSkipped(skipped)
    // the button goes away, so focus moves to the toolbar's first control
    requestAnimationFrame(() => fileToolbar.current?.querySelector<HTMLElement>('button:not([disabled])')?.focus())
  }
  const save = () => {
    saveGraphFile(graph, fileName)
    editing?.onSaved()
  }
  return (
    <>
      <HeaderContent>
        <div className={styles.file}>
          <h1 className={styles.fileName}>{fileName}</h1>
          {editing?.edited && <Badge>Edited</Badge>}
          <span role="status" className={styles.note}>
            {editing?.storageFailed ? 'Changes are not kept after reload in this browser.' : (announcement ?? '')}
          </span>
        </div>
        {editing && <HistoryButtons history={editing.history} />}
        <div ref={fileToolbar} className={styles.toolbarGroup}>
          <Toolbar label="File">
            {toolbar}
            {editing && recommendations.length > 0 && (
              <Button size="sm" onPress={acceptAll}>
                <Check aria-hidden="true" />
                Accept all recommendations ({recommendations.length})
              </Button>
            )}
            <Button size="sm" onPress={save} isDisabled={editing !== undefined && editing.canSave === false}>
              <Save aria-hidden="true" />
              Save as file
            </Button>
            <Button
              size="sm"
              onPress={() => zip.mutate()}
              isDisabled={zip.isPending || blocked !== null}
              aria-describedby={blocked ? reasonId : undefined}
            >
              <Download aria-hidden="true" />
              {zip.isPending ? 'Rendering…' : 'Download zip'}
            </Button>
            {mode === 'managed' && (
              <Button
                size="sm"
                variant="primary"
                onPress={() => setReviewing(true)}
                isDisabled={blocked !== null}
                aria-describedby={blocked ? reasonId : undefined}
              >
                <Rocket aria-hidden="true" />
                Deploy
              </Button>
            )}
            {blocked && (
              <span id={reasonId} className={styles.reason}>
                {blocked}
              </span>
            )}
          </Toolbar>
        </div>
        <span role="status" className={styles.note}>
          {skipped > 0
            ? `${skipped} ${skipped === 1 ? 'recommendation was' : 'recommendations were'} skipped: the instance or its provider is gone.`
            : ''}
        </span>
      </HeaderContent>
      {reviewing && <ReviewDialog graph={graph} onClose={() => setReviewing(false)} />}
      <ReactFlowProvider>
        <Workspace
          catalog={<CatalogPanel entries={catalog.data.modules} onAdd={editing?.onAdd && ((m) => editing.onAdd?.(m, catalog.data))} />}
          canvas={
            <div className={styles.canvas}>
              {notice && (
                <p className={styles.notice}>
                  <Info aria-hidden="true" />
                  <span>{notice}</span>
                </p>
              )}
              {overlay && <div className={styles.overlay}>{overlay}</div>}
              {zip.isError && <Failure error={zip.error} onRetry={() => zip.mutate()} />}
              <GraphView
                graph={graph}
                catalog={catalog.data}
                problems={shownResult.problems}
                positions={editing?.positions}
                onEdit={editing?.onEdit}
              />
            </div>
          }
          inspector={
            <Inspector
              graph={graph}
              catalog={catalog.data}
              check={shownResult}
              checkCurrent={checkState === 'current'}
              onEdit={editing?.onEdit}
            />
          }
          problemsStatus={
            <span role="status" className={styles.status}>
              {checkState === 'checking' ? 'Checking the graph…' : checkState === 'failed' ? 'Not checked' : ''}
            </span>
          }
          problems={
            checkState === 'failed' && editing?.checkError ? (
              // a 400 means the renderer refused this graph: retrying cannot help, undo is the way back
              <Failure error={editing.checkError} onRetry={editing.checkError.status === 400 ? undefined : editing.onRetry} />
            ) : (
              <ProblemsPanel problems={shownResult.problems} graph={graph} />
            )
          }
        />
      </ReactFlowProvider>
    </>
  )
}

/**
 * Undo, redo and Tidy layout, with the undo and redo shortcuts (AC-15). Without history (the
 * sample) every button is disabled and the shortcuts do nothing. The shortcuts never act while
 * focus is in a text field or select.
 */
function HistoryButtons({ history }: { history?: HistoryControls }) {
  const apple = isApple()
  const latest = useRef(history)
  latest.current = history

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const h = latest.current
      if (!h || e.defaultPrevented || isFormControl(e.target)) return
      const step = historyKey(e, apple)
      if (!step) return
      e.preventDefault()
      if (step === 'undo' && h.canUndo) {
        h.onUndo()
        // an undone add removes the focused node; focus then moves to the canvas region (spec 0029, AC-5)
        requestAnimationFrame(() => {
          if (document.activeElement === null || document.activeElement === document.body) {
            document.querySelector<HTMLElement>('[data-workspace] section[aria-label^="Graph"]')?.focus()
          }
        })
      }
      if (step === 'redo' && h.canRedo) h.onRedo()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [apple])

  const undo = historyShortcut('undo', apple)
  const redo = historyShortcut('redo', apple)
  return (
    <Toolbar label="History" divider>
      <IconButton
        label="Undo"
        hint={undo.label}
        aria-keyshortcuts={undo.aria}
        size="sm"
        isDisabled={!history?.canUndo}
        onPress={history?.onUndo}
      >
        <Undo2 aria-hidden="true" />
      </IconButton>
      <IconButton
        label="Redo"
        hint={redo.label}
        aria-keyshortcuts={redo.aria}
        size="sm"
        isDisabled={!history?.canRedo}
        onPress={history?.onRedo}
      >
        <Redo2 aria-hidden="true" />
      </IconButton>
      <Button size="sm" variant="ghost" isDisabled={!history} onPress={history?.onTidy}>
        <LayoutGrid aria-hidden="true" />
        Tidy layout
      </Button>
    </Toolbar>
  )
}
