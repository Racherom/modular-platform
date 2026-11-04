import { ChevronDown, ChevronUp } from 'lucide-react'
import { createContext, type ReactNode, useContext, useId, useState } from 'react'
import { createPortal } from 'react-dom'
import styles from './AppShell.module.css'
import { Button } from './ui/Button'
import { Panel } from './ui/Panel'
import { ThemeToggle } from './ui/ThemeToggle'

const HeaderSlot = createContext<HTMLElement | null>(null)

interface AppShellProps {
  /** The main navigation. */
  nav: ReactNode
  /** The signed in account, in managed mode. */
  account?: ReactNode
  children: ReactNode
}

/** The page frame: the header (app name, navigation, the screen's own part, theme) and main. */
export function AppShell({ nav, account, children }: AppShellProps) {
  const [slot, setSlot] = useState<HTMLElement | null>(null)
  return (
    <div className={styles.app}>
      <header className={styles.header}>
        <span className={styles.name}>Modular Platform</span>
        {nav}
        <div ref={setSlot} className={styles.slot} />
        <div className={styles.end}>
          <ThemeToggle />
          {account}
        </div>
      </header>
      <main className={styles.main}>
        <HeaderSlot.Provider value={slot}>{children}</HeaderSlot.Provider>
      </main>
    </div>
  )
}

/** Puts a screen's file name and toolbar into the header. */
export function HeaderContent({ children }: { children: ReactNode }) {
  const slot = useContext(HeaderSlot)
  return slot ? createPortal(children, slot) : null
}

interface WorkspaceProps {
  catalog: ReactNode
  canvas: ReactNode
  inspector: ReactNode
  /** The problems panel's body. */
  problems: ReactNode
  /** Shown beside the problems heading, such as "Checking the graph". */
  problemsStatus?: ReactNode
}

/**
 * The editor's four regions (AC-6): catalog left, canvas in the middle, inspector right,
 * problems below the canvas. Below 1024px they stack under the canvas.
 */
export function Workspace({ catalog, canvas, inspector, problems, problemsStatus }: WorkspaceProps) {
  const [open, setOpen] = useState(true)
  const bodyId = useId()
  return (
    <div className={styles.workspace} data-workspace="">
      <div className={styles.catalog}>{catalog}</div>
      <div className={styles.canvas}>{canvas}</div>
      <Panel
        title="Problems"
        className={styles.problems}
        collapsed={!open}
        bodyId={bodyId}
        actions={
          <>
            {problemsStatus}
            <Button variant="ghost" size="sm" aria-expanded={open} aria-controls={bodyId} onPress={() => setOpen((o) => !o)}>
              {open ? <ChevronDown aria-hidden="true" /> : <ChevronUp aria-hidden="true" />}
              {open ? 'Hide' : 'Show'}
            </Button>
          </>
        }
      >
        {problems}
      </Panel>
      <div className={styles.inspector}>{inspector}</div>
    </div>
  )
}
