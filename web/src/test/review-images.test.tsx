import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { type ReviewImage, ReviewImages, shortDigest, usedByText } from '../components/ReviewImages'

// The review's Images section (spec 0031): one row per reference, what runs it, and its badge.
const d = (c: string) => `sha256:${c.repeat(64)}`

const images: ReviewImage[] = [
  {
    reference: 'postgres:18.6',
    usedBy: [
      { kind: 'provision', instance: 'db', capability: 'postgresql-provisioner', consumer: 'app' },
      { kind: 'revoke', instance: 'db', capability: 'postgresql-provisioner' },
    ],
    digest: d('a'),
    change: 'frozen',
    present: false,
  },
  {
    reference: 'traefik/whoami:v1.12.0',
    usedBy: [{ kind: 'module', instance: 'web' }],
    digest: d('b'),
    change: 'frozen',
    available: d('c'),
    present: true,
  },
  { reference: 'valkey:8', usedBy: [{ kind: 'module', instance: 'cache' }], digest: d('e'), change: 'new', present: true },
  {
    reference: 'nginx:1.29',
    usedBy: [{ kind: 'module', instance: 'proxy' }],
    digest: d('f'),
    change: 'updated',
    previousDigest: d('1'),
    present: true,
  },
]

function show(update: string[] = []) {
  const onToggle = vi.fn()
  const onCheckNewer = vi.fn()
  render(<ReviewImages images={images} update={update} checking={false} onToggle={onToggle} onCheckNewer={onCheckNewer} />)
  return { onToggle, onCheckNewer }
}

describe('ReviewImages', () => {
  it('names what runs each image, jobs by capability, provider and consumer', () => {
    expect(usedByText({ kind: 'module', instance: 'web' })).toBe('web')
    expect(usedByText({ kind: 'provision', instance: 'db', capability: 'postgresql-provisioner', consumer: 'app' })).toBe(
      'provision job of postgresql-provisioner on db for app',
    )
    expect(usedByText({ kind: 'revoke', instance: 'db', capability: 'postgresql-provisioner' })).toBe(
      'revoke job of postgresql-provisioner on db',
    )
    show()
    const row = screen.getByTestId('image-postgres:18.6')
    expect(within(row).getByText('used by provision job of postgresql-provisioner on db for app')).toBeTruthy()
    expect(within(row).getByText('used by revoke job of postgresql-provisioner on db')).toBeTruthy()
  })

  it('shows a short digest and the badge of each change', () => {
    expect(shortDigest(d('a'))).toBe('aaaaaaaaaaaa')
    show()
    const frozen = screen.getByTestId('image-postgres:18.6')
    expect(within(frozen).getByText('frozen')).toBeTruthy()
    expect(within(frozen).getByText('not on this host, pulled by digest at apply')).toBeTruthy()
    expect(within(screen.getByTestId('image-traefik/whoami:v1.12.0')).getByText('newer available')).toBeTruthy()
    expect(within(screen.getByTestId('image-valkey:8')).getByText('new')).toBeTruthy()
    expect(within(screen.getByTestId('image-nginx:1.29')).getByText(/updating 111111111111 → ffffffffffff/)).toBeTruthy()
  })

  it('offers a tick box only where a newer version exists or was taken', () => {
    const { onToggle, onCheckNewer } = show(['nginx:1.29'])
    const boxes = screen.getAllByRole('checkbox')
    expect(boxes).toHaveLength(2)
    fireEvent.click(screen.getByLabelText('Take the newer version of traefik/whoami:v1.12.0'))
    expect(onToggle).toHaveBeenCalledWith('traefik/whoami:v1.12.0', true)
    fireEvent.click(screen.getByLabelText('Take the newer version of nginx:1.29'))
    expect(onToggle).toHaveBeenCalledWith('nginx:1.29', false)
    fireEvent.click(screen.getByRole('button', { name: 'Check for newer images' }))
    expect(onCheckNewer).toHaveBeenCalled()
  })
})
