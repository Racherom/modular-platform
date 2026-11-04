import { useRef, useState } from 'react'
import { saveGraphFile } from '../lib/download'
import { useEditor } from '../lib/store'
import { Button } from './ui/Button'
import { Dialog } from './ui/Dialog'

/** What waits behind the replace prompt: the step to run, and what to undo on Cancel. */
interface Pending {
  run: () => void
  cancel?: () => void
}

/**
 * Guards a step that replaces the working graph (AC-18): when the working graph carries the
 * edited flag, a Dialog first offers Save as file (saves it, then goes on), Replace and Cancel.
 * Otherwise the step runs at once. Render `dialog` once on the screen.
 */
export function useReplaceGuard() {
  const [pending, setPending] = useState<Pending | null>(null)
  const working = useEditor((s) => s.working)
  // the step runs inside the press that answered, so a step that opens a file chooser keeps the
  // browser's user activation; the flag stops a second answer (a press, then the close) running it twice
  const answered = useRef(false)

  const guard = (run: () => void, cancel?: () => void) => {
    if (useEditor.getState().working?.edited) {
      answered.current = false
      setPending({ run, cancel })
    } else run()
  }

  const finish = (go: boolean) => {
    if (!pending || answered.current) return
    answered.current = true
    setPending(null)
    if (go) pending.run()
    else pending.cancel?.()
  }

  const dialog = (
    <Dialog
      isOpen={pending !== null}
      onClose={() => finish(false)}
      role="alertdialog"
      title="Replace the edited graph?"
      actions={
        <>
          {/* the safe answer takes focus first */}
          <Button autoFocus onPress={() => finish(false)}>
            Cancel
          </Button>
          <Button
            onPress={() => {
              const w = useEditor.getState().working
              if (w) {
                saveGraphFile(w.graph, w.fileName)
                useEditor.getState().saved()
              }
              finish(true)
            }}
          >
            Save as file
          </Button>
          <Button variant="danger" onPress={() => finish(true)}>
            Replace
          </Button>
        </>
      }
    >
      <p>
        <code>{working?.fileName}</code> has changes you have not saved as a file. Replacing it drops those changes.
      </p>
      <p>Save as file keeps a copy first.</p>
    </Dialog>
  )

  return { guard, dialog }
}
