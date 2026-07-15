import { ref } from 'vue'
import { readStorage, writeStorage } from '../platform/storage'

export interface ShortcutDefinition {
  id: string
  defaultKeys: string[]
}

/** Owns shortcut preference state and keyboard-capture cleanup-free logic. */
export function usePlayerShortcuts(actions: ShortcutDefinition[]) {
  const shortcutMap = ref<Record<string, string[]>>({})
  const editingShortcutId = ref<string | null>(null)

  function defaults(): Record<string, string[]> {
    return Object.fromEntries(actions.map((action) => [action.id, [...action.defaultKeys]]))
  }

  function load(): void {
    shortcutMap.value = readStorage<Record<string, string[]>>('cczj_shortcuts', defaults())
  }

  function save(): void {
    writeStorage('cczj_shortcuts', shortcutMap.value)
  }

  function startEditShortcut(id: string): void {
    editingShortcutId.value = id
  }

  function cancelEditShortcut(): void {
    editingShortcutId.value = null
  }

  function onShortcutKeyDown(event: KeyboardEvent): void {
    const id = editingShortcutId.value
    if (!id) return
    event.preventDefault()
    event.stopPropagation()
    if (event.key === 'Escape') {
      cancelEditShortcut()
      return
    }
    shortcutMap.value[id] = [event.key === ' ' ? 'Space' : event.key]
    save()
    editingShortcutId.value = null
  }

  function resetShortcut(id: string): void {
    const action = actions.find((item) => item.id === id)
    if (!action) return
    shortcutMap.value[id] = [...action.defaultKeys]
    save()
  }

  function resetAllShortcuts(): void {
    shortcutMap.value = defaults()
    save()
  }

  function fmtKey(key: string): string {
    const names: Record<string, string> = {
      Space: '空格', ArrowLeft: '←', ArrowRight: '→', ArrowUp: '↑', ArrowDown: '↓',
      Escape: 'Esc', Enter: 'Enter', Tab: 'Tab',
    }
    return names[key] ?? (key.length === 1 ? key.toUpperCase() : key)
  }

  return {
    shortcutMap,
    editingShortcutId,
    load,
    startEditShortcut,
    cancelEditShortcut,
    onShortcutKeyDown,
    resetShortcut,
    resetAllShortcuts,
    fmtKey,
  }
}
