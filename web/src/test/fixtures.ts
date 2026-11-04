// Test helpers for v1 catalogs (spec 0011): a module whose provides and slots are each named like
// their capability, the common case of the built in catalog.
import type { Catalog, Module } from '../api/client'

interface Shape {
  description?: string
  image?: string
  runtime?: 'sdk'
  /** capability to its port */
  provides?: Record<string, { port?: number; version?: string }>
  /** slot (named like its capability) to whether it is optional */
  uses?: Record<string, { optional?: boolean; capability?: string }>
}

/** A v1 module with one provide per capability (version 1.0 unless given) and one slot per capability. */
export function testModule(name: string, shape: Shape = {}): Module {
  const m: Module = {
    apiVersion: 'modular-platform/v1',
    kind: 'Module',
    name,
    description: shape.description ?? name,
    image: shape.image ?? `${name}:1`,
  }
  if (shape.runtime) m.runtime = shape.runtime
  if (shape.provides) {
    m.provides = Object.fromEntries(
      Object.entries(shape.provides).map(([c, p]) => [
        c,
        { capability: c, version: p.version ?? '1.0', ...(p.port ? { port: p.port } : {}) },
      ]),
    ) as Module['provides']
  }
  if (shape.uses) {
    m.uses = Object.fromEntries(
      Object.entries(shape.uses).map(([s, u]) => [s, { capability: u.capability ?? s, optional: u.optional ?? false, per_caller: false }]),
    ) as Module['uses']
  }
  return m
}

/** A catalog of modules and, when given, capabilities. */
export function testCatalog(modules: Module[], capabilities: Catalog['capabilities'] = []): Catalog {
  return { modules, capabilities }
}
