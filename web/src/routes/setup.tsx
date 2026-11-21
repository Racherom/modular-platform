import { createFileRoute } from '@tanstack/react-router'
import { Server } from 'lucide-react'
import { SetupPage } from '../components/Setup'
import { EmptyState } from '../components/ui/EmptyState'
import { mode } from '../lib/config'

export const Route = createFileRoute('/setup')({ component: Setup })

function Setup() {
  if (mode !== 'managed') {
    return <EmptyState icon={Server} level={1} title="The setup belongs to the managed platform" />
  }
  return <SetupPage />
}
