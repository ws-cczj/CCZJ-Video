type Next = (...args: unknown[]) => unknown
type Wrapper = (next: Next, ...args: unknown[]) => unknown
interface Layer { wrapper: Wrapper | null; previous: Layer | null }
interface Chain { original: Function; dispatch: Function; head: Layer | null }
const chains = new WeakMap<object, Map<string, Chain>>()

/** A single dispatcher supports arbitrary pack removal without stale wrappers. */
export function interceptMethod(target: object, method: string, wrapper: Wrapper): () => void {
  const holder = target as Record<string, unknown>
  let methods = chains.get(target)
  if (!methods) { methods = new Map(); chains.set(target, methods) }
  let chain = methods.get(method)
  if (!chain || holder[method] !== chain.dispatch) {
    const original = holder[method]
    if (typeof original !== 'function') throw new Error(`${method} 不是一个方法，没法包住它`)
    const owned: Chain = { original, head: null, dispatch: () => {} }
    owned.dispatch = function (this: unknown, ...args: unknown[]) {
      const receiver = this
      const invoke = (layer: Layer | null, values: unknown[]): unknown => {
        if (!layer) return owned.original.apply(receiver, values)
        const next = (...nextArgs: unknown[]) => invoke(layer.previous, nextArgs)
        return layer.wrapper ? layer.wrapper(next, ...values) : next(...values)
      }
      return invoke(owned.head, args)
    }
    chain = owned
    methods.set(method, chain)
    holder[method] = chain.dispatch
  }
  const owned = chain
  const layer: Layer = { wrapper, previous: owned.head }
  owned.head = layer
  return () => {
    if (!layer.wrapper) return
    layer.wrapper = null
    if (owned.head === layer) owned.head = layer.previous
    else {
      for (let above = owned.head; above; above = above.previous) {
        if (above.previous === layer) { above.previous = layer.previous; break }
      }
    }
    if (!owned.head) {
      if (holder[method] === owned.dispatch) holder[method] = owned.original
      if (methods!.get(method) === owned) methods!.delete(method)
      if (!methods!.size) chains.delete(target)
    }
  }
}
