import { ref } from 'vue'

/** 收藏列表刷新信号：Detail/Player 添加收藏后递增，Favorites 页监听并重新加载 */
export const favRefreshTick = ref(0)

export function bumpFavoritesRefresh(): void {
  favRefreshTick.value++
}
