import type { components } from '../api/api.gen'
import m from './Managed.module.css'
import { Badge } from './ui/Badge'
import { Button } from './ui/Button'

/** One image row of the review (spec 0031). */
export type ReviewImage = components['schemas']['ReviewImage']
/** One thing that runs an image: a module instance, a provision job or a revoke job. */
export type ReviewImageUser = components['schemas']['ReviewImageUser']

/** The first 12 hex of a digest, as `docker image ls` shows an ID. */
export function shortDigest(digest: string | undefined): string {
  return (digest ?? '').replace(/^sha256:/, '').slice(0, 12)
}

/** What runs an image, in words: the instance for a module, the job with its capability, provider and consumer. */
export function usedByText(u: ReviewImageUser): string {
  switch (u.kind) {
    case 'provision':
      return `provision job of ${u.capability} on ${u.instance} for ${u.consumer}`
    case 'revoke':
      return `revoke job of ${u.capability} on ${u.instance}`
    default:
      return u.instance ?? ''
  }
}

function ImageBadge({ image }: { image: ReviewImage }) {
  switch (image.change) {
    case 'new':
      return <Badge tone="info">new</Badge>
    case 'updated':
      return (
        <Badge tone="warning">
          updating {shortDigest(image.previousDigest)} → {shortDigest(image.digest)}
        </Badge>
      )
    default:
      return (
        <>
          <Badge>frozen</Badge>
          {image.available && <Badge tone="info">newer available</Badge>}
          {!image.present && <Badge tone="warning">not on this host, pulled by digest at apply</Badge>}
        </>
      )
  }
}

/**
 * The review's Images section (spec 0031): one row per image reference with what runs it, the
 * short digest the apply runs, and its badge. A row with a newer version (or one you already
 * took) has a tick box; ticking or unticking reviews again with the new update list.
 */
export function ReviewImages({
  images,
  update,
  checking,
  onCheckNewer,
  onToggle,
}: {
  images: ReviewImage[]
  update: string[]
  checking: boolean
  onCheckNewer: () => void
  onToggle: (reference: string, take: boolean) => void
}) {
  return (
    <>
      <h3>Images</h3>
      <ul className={m.list} aria-label="Images">
        {images.map((img) => {
          const ticked = update.includes(img.reference)
          return (
            <li key={img.reference} data-testid={`image-${img.reference}`}>
              <code>{img.reference}</code> at <code title={img.digest}>{shortDigest(img.digest)}</code> <ImageBadge image={img} />
              <ul className={m.list}>
                {img.usedBy.map((u) => (
                  <li key={`${u.kind}.${u.instance}.${u.capability}.${u.consumer}`}>used by {usedByText(u)}</li>
                ))}
              </ul>
              {(img.available || ticked) && (
                <label className={m.check}>
                  <input type="checkbox" checked={ticked} disabled={checking} onChange={(e) => onToggle(img.reference, e.target.checked)} />{' '}
                  Take the newer version of {img.reference}
                </label>
              )}
            </li>
          )
        })}
      </ul>
      <Button onPress={onCheckNewer} isDisabled={checking}>
        Check for newer images
      </Button>
    </>
  )
}
