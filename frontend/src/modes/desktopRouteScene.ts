import { defineComponent, h, type Component, type PropType, type VNode } from 'vue'

/** Cache scenes by view type so changing routes does not recreate KeepAlive's
 * component definitions. A page may have sibling overlays (a Fragment); only
 * this element root receives the route's enter/leave transition. */
export function createDesktopRouteScenes(): (view: VNode) => Component {
  const scenes = new Map<VNode['type'], Component>()
  return view => {
    const existing = scenes.get(view.type)
    if (existing) return existing
    const source = view.type as { name?: string; __name?: string }
    const scene = defineComponent({
      // KeepAlive's existing include list still selects the same pages.
      name: source.name || source.__name || 'DesktopRoutePage',
      props: { view: { type: Object as PropType<VNode>, required: true } },
      setup: props => () => h('div', { class: 'desktop-route-scene' }, [props.view]),
    })
    scenes.set(view.type, scene)
    return scene
  }
}
