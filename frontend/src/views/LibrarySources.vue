<script setup lang="ts">
/**
 * 源管理页的外壳。小说与漫画各有一条路由（/novel/sources、/comic/sources），
 * 但面板是同一份，只是 kind 不同——所以这里唯一的职责是从路由推出 kind。
 *
 * 用路由前缀而不是 meta 字段：路由守卫已经按前缀区分模式了（ADR 0014），
 * 再引入一份 meta 就等于同一个事实存两处，改一处忘一处。
 */
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import LibrarySourcesPanel from '../components/LibrarySourcesPanel.vue'

const route = useRoute()
const kind = computed<'novel' | 'comic'>(() => (route.path.startsWith('/comic') ? 'comic' : 'novel'))
</script>

<template>
  <div class="library-sources-page cczj-w-full cczj-mx-auto cczj-p-5">
    <LibrarySourcesPanel :key="kind" :kind="kind" />
  </div>
</template>

<style scoped>
.library-sources-page {
  padding: 20px;
  max-width: 860px;
  margin: 0 auto;
}
</style>
