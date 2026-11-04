import { useMutation } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { FolderOpen } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { planGraph } from '../api/client'
import { Failure } from '../components/Failure'
import { GraphScreen, useCatalog } from '../components/GraphScreen'
import { Button } from '../components/ui/Button'
import { bind } from '../graph/edit'
import { MAX_FILE_BYTES, parseGraph } from '../graph/yaml'
import { loadWorking, type WorkingGraph } from '../lib/storage'
import { useEditor } from '../lib/store'

export const Route = createFileRoute('/')({ component: Editor })

function Editor() {
  const working = useEditor((s) => s.working)
  const planState = useEditor((s) => s.planState)
  const planError = useEditor((s) => s.planError)
  const { restore, open: openWorking, edit, retry, saved } = useEditor.getState()
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

  // one plan per open: it validates the file and writes the automatic bindings into the graph
  const open = useMutation({
    mutationFn: async (file: File): Promise<WorkingGraph> => {
      if (file.size > MAX_FILE_BYTES) throw new Error('The file is over 1 MB.')
      const plan = await planGraph(parseGraph(await file.text()))
      return { fileName: file.name, graph: plan.graph, plan }
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
      <Button size="sm" onPress={() => input.current?.click()} isDisabled={open.isPending}>
        <FolderOpen aria-hidden="true" />
        {open.isPending ? 'Opening…' : 'Open file'}
      </Button>
    </>
  )

  if (working === undefined) return <p>Loading…</p>
  return (
    <>
      {open.isError && <Failure error={open.error} onRetry={() => input.current?.click()} />}
      {working ? (
        <GraphScreen
          key={opens}
          graph={working.graph}
          plan={working.plan}
          fileName={working.fileName}
          toolbar={opener}
          editing={{
            positions: working.positions ?? {},
            edited: working.edited ?? false,
            planState,
            planError,
            onRetry: retry,
            onBind: (consumer, capability, provider) => edit((g) => bind(g, consumer, capability, provider)),
            onSaved: saved,
          }}
        />
      ) : (
        <div className="empty">
          <h1>No graph open</h1>
          <p>Open a graph.yaml file, or start from the sample.</p>
          <div className="empty__actions">{opener}</div>
          {catalog.isError && <Failure error={catalog.error} onRetry={() => void catalog.refetch()} />}
        </div>
      )}
    </>
  )
}
