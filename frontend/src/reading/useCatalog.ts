import { computed, onMounted, watch } from 'vue'
import { fetchCatalogPage, type CatalogPageDTO, type LibraryKind } from '../api/library'
import { useAsyncResource, type AsyncResource } from './asyncResource'
import { startupTask } from '../diagnostics/startup'

/** Owns bounded local-catalog paging; filters always apply to the whole catalog. */
export function useCatalog(kind: LibraryKind, query: () => { genre?: string; complete?: boolean; sort?: string }, enabled: () => boolean = () => true) {
  const resource: AsyncResource<CatalogPageDTO, [string]> = useAsyncResource<CatalogPageDTO, [string]>({
    diagnosticLabel:`${kind}.catalog`,
    load: cursor => {
      const previous: CatalogPageDTO | null = resource.data.value
      return fetchCatalogPage(kind, { ...query(), cursor, limit: 60, includeRetired: true }).then(page => {
        if (!cursor || !previous) return page
        const indexed = new Map(previous.items.map(item => [item.id, item]))
        for (const item of page.items) indexed.set(item.id, item)
        return { ...page, items: [...indexed.values()] }
      })
    },
    isEmpty: page => page.items.length === 0,
  })
  async function reload(): Promise<void> {
    resource.cancel()
    resource.data.value = null
    if (enabled()) await resource.run('')
  }
  watch(() => JSON.stringify([enabled(), query()]), reload)
  onMounted(()=>{if(enabled())void startupTask('first_screen',reload)})
  const items = computed(() => resource.data.value?.items ?? [])
  const genres = computed(() => resource.data.value?.genres ?? [])
  const loading = computed(() => resource.status.value === 'loading')
  const status = computed(() => items.value.length && ['loading', 'error'].includes(resource.status.value) ? 'ready' : resource.status.value)
  const nextError = computed(() => items.value.length && resource.status.value === 'error' ? resource.error.value : null)
  const hasNext = computed(() => !!resource.data.value?.nextCursor)
  function more(): void { if (!loading.value && hasNext.value) void resource.run(resource.data.value!.nextCursor) }
  return { ...resource, status, items, genres, loading, nextError, hasNext, reload, more }
}
