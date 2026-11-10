import { useMutation } from '@tanstack/react-query'
import { createFileRoute, Link } from '@tanstack/react-router'
import { FolderOpen, Workflow } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { checkGraph } from '../api/client'
import { GraphScreen, useCatalog } from '../components/GraphScreen'
import { useReplaceGuard } from '../components/ReplaceDialog'
import { Button } from '../components/ui/Button'
import { EmptyState } from '../components/ui/EmptyState'
import { Failure } from '../components/ui/Failure'
import { MAX_FILE_BYTES, parseGraph } from '../graph/yaml'
import { loadWorking, type WorkingGraph } from '../lib/storage'
import { useEditor } from '../lib/store'

export const Route = createFileRoute('/')({ component: Editor })

function Editor() {
  const working = useEditor((s) => s.working)
  const checkState = useEditor((s) => s.checkState)
  const checkError = useEditor((s) => s.checkError)
  const storageFailed = useEditor((s) => s.storageFailed)
  const canUndo = useEditor((s) => s.history.past.length > 0)
  const canRedo = useEditor((s) => s.history.future.length > 0)
  const { restore, open: openWorking, edit, retry, saved, undo, redo, tidy } = useEditor.getState()
  const replace = useReplaceGuard()
  const catalog = useCatalog()
  const input = useRef<HTMLInputElement>(null)
  // counts opens, so reopening a file with the same names still starts a fresh screen
  const [opens, setOpens] = useState(0)

  useEffect(() => {
    if (working !== undefined) return
    // never let a slow read replace a graph that is already set
    loadWorking()
      .then((w) => restore(w ?? null))
      .catch(() => restore(null))
  }, [working, restore])

  // the sample copy brings its selection along; focus follows a module so the keyboard is not lost
  useEffect(() => {
    const { takeHandoff, focusModule, select } = useEditor.getState()
    const handoff = takeHandoff()
    if (handoff?.kind === 'module') focusModule(handoff.instance)
    else if (handoff) select(handoff)
  }, [])

  // one check per open: it validates the file and writes the automatic bindings into the graph
  const open = useMutation({
    mutationFn: async (file: File): Promise<WorkingGraph> => {
      if (file.size > MAX_FILE_BYTES) throw new Error('The file is over 1 MB.')
      const check = await checkGraph(parseGraph(await file.text()))
      return { fileName: file.name, graph: check.graph, check }
    },
    onSuccess: (w) => {
      setOpens((n) => n + 1)
      openWorking(w)
    },
  })

  const opener = (
    <>
      <input
        ref={input}
        type="file"
        accept=".yaml,.yml,application/yaml"
        hidden
        onChange={(e) => {
          const file = e.target.files?.[0]
          if (file) open.mutate(file)
          e.target.value = ''
        }}
      />
      {/* AC-18: an edited working graph asks first, before any file is read */}
      <Button size="sm" onPress={() => replace.guard(() => input.current?.click())} isDisabled={open.isPending}>
        <FolderOpen aria-hidden="true" />
        {open.isPending ? 'Opening…' : 'Open file'}
      </Button>
    </>
  )

  if (working === undefined) return <p>Loading…</p>
  return (
    <>
      {replace.dialog}
      {open.isError && <Failure error={open.error} onRetry={() => input.current?.click()} />}
      {working ? (
        <GraphScreen
          key={opens}
          graph={working.graph}
          check={working.check}
          fileName={working.fileName}
          toolbar={opener}
          editing={{
            positions: working.positions ?? {},
            edited: working.edited ?? false,
            checkState,
            checkError,
            onRetry: retry,
            onEdit: edit,
            onSaved: saved,
            history: { canUndo, canRedo, onUndo: undo, onRedo: redo, onTidy: tidy },
            storageFailed,
          }}
        />
      ) : (
        <EmptyState icon={Workflow} level={1} title="No graph open" actions={opener}>
          <p>
            Open a <code>graph.yaml</code> file, or{' '}
            <Link to="/samples/$name" params={{ name: 'whoami-postgres' }}>
              start from the sample
            </Link>
            .
          </p>
          {catalog.isError && <Failure error={catalog.error} onRetry={() => void catalog.refetch()} />}
        </EmptyState>
      )}
    </>
  )
}
