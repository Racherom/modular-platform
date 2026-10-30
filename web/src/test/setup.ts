import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

// React Flow measures with ResizeObserver and DOMMatrixReadOnly, which jsdom lacks.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
globalThis.ResizeObserver ??= ResizeObserverStub as unknown as typeof ResizeObserver

class DOMMatrixStub {
  m22: number
  constructor(transform?: string) {
    const scale = transform?.match(/scale\(([1-9.])\)/)?.[1]
    this.m22 = scale !== undefined ? Number(scale) : 1
  }
}
globalThis.DOMMatrixReadOnly ??= DOMMatrixStub as unknown as typeof DOMMatrixReadOnly

/** Tests answer the editor's fetch calls by setting this. openapi-fetch keeps the fetch it saw at start. */
export const fetchHandler: { current: (req: Request) => Promise<Response> } = {
  current: () => Promise.reject(new Error('no fetch handler set')),
}
globalThis.fetch = (input: RequestInfo | URL, init?: RequestInit) =>
  fetchHandler.current(input instanceof Request ? input : new Request(new URL(String(input), 'http://test'), init))

afterEach(() => cleanup())
