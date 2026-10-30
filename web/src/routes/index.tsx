import { useMutation } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { useEffect, useRef, useState } from 'react'
import { planGraph } from '../api/client'
import { Failure } from '../components/Failure'
import { GraphScreen, useCatalog } from '../components/GraphScreen'
import { MAX_FILE_BYTES, parseGraph } from '../graph/yaml'
import { loadWorking, saveWorking, type WorkingGraph } from '../lib/storage'
import { useEditor } from '../lib/store'

export const Route = createFileRoute('/')({ component: Editor })

function Editor() {
  const { working, setWorking } = useEditor()
  const catalog = useCatalog()
  const input = useRef<HTMLInputElement>(null)
  // counts opens, so reopening a file with the same names still starts a fresh screen
  const [opens, setOpens] = useState(0)

  useEffect(() => {
    if (working !== undefined) return
    // never let a slow read replace a graph that is already set
    const settle = (w: WorkingGraph | null) => {
      if (useEditor.getState().working === undefined) setWorking(w)
    }
    loadWorking()
      .then((w) => settle(w ?? null))
      .catch(() => settle(null))
  }, [working, setWorking])

  // one plan per open: it validates the file and writes the automatic bindings into the graph
  const open = useMutation({
    mutationFn: async (file: File): Promise<WorkingGraph> => {
      if (file.size > MAX_FILE_BYTES) throw new Error('The file is over 1 MB.')
      const plan = await planGraph(parseGraph(await file.text()))
      return { fileName: file.name, graph: plan.graph, plan }
    },
    onSuccess: (w) => {
      setOpens((n) => n + 1)
      setWorking(w)
      saveWorking(w).catch(() => undefined) // a private window may refuse storage; the graph still shows
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
      <button type="button" onClick={() => input.current?.click()} disabled={open.isPending}>
        {open.isPending ? 'Opening…' : 'Open file'}
      </button>
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
        />
      ) : (
        <div className="screen empty">
          <h1>No graph open</h1>
          <p>Open a graph.yaml file, or start from the sample.</p>
          <div className="toolbar">{opener}</div>
          {catalog.isError && <Failure error={catalog.error} onRetry={() => void catalog.refetch()} />}
        </div>
      )}
    </>
  )
}
