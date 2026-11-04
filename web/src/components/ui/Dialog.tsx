import type { ReactNode } from 'react'
import { Dialog as AriaDialog, Heading, Modal, ModalOverlay } from 'react-aria-components'
import styles from './Dialog.module.css'

/** Props of {@link Dialog}. */
export interface DialogProps {
  isOpen: boolean
  /** Called on Escape and on a click outside; the dialog does not close itself otherwise. */
  onClose: () => void
  /** The heading, which also names the dialog. */
  title: string
  /** `alertdialog` for a question that needs an answer before you go on. */
  role?: 'dialog' | 'alertdialog'
  /** The buttons, in reading order; the last one is the main action. */
  actions: ReactNode
  children: ReactNode
}

/**
 * A modal dialog on React Aria Components: focus stays inside while it is open, Escape closes
 * it, and focus returns to the control that opened it (AC-4).
 */
export function Dialog({ isOpen, onClose, title, role = 'dialog', actions, children }: DialogProps) {
  return (
    <ModalOverlay
      className={styles.overlay}
      isOpen={isOpen}
      isDismissable
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <Modal className={styles.modal}>
        <AriaDialog role={role} className={styles.dialog}>
          <Heading slot="title" className={styles.title}>
            {title}
          </Heading>
          <div className={styles.body}>{children}</div>
          <div className={styles.actions}>{actions}</div>
        </AriaDialog>
      </Modal>
    </ModalOverlay>
  )
}
