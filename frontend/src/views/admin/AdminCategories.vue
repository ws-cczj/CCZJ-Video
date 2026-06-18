<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { GetTypes, GetAllSources } from '../../../bindings/cczjVideo/app'
import { Empty } from '../../components/ui'

interface VideoType {
  id: number; type_name: string; parent_id: number; sort: number; source_key: string
}

const allTypes = ref<VideoType[]>([])
const typeSourceKey = ref('')
const sourceKeys = ref<string[]>([])

// 树形分组：parent_id=0 的为顶级，其余挂在对应 parent 下
const treeTypes = computed(() => {
  const map = new Map<number, VideoType & { children: VideoType[] }>()
  const roots: (VideoType & { children: VideoType[] })[] = []
  for (const t of allTypes.value) {
    map.set(t.id, { ...t, children: [] })
  }
  for (const t of allTypes.value) {
    const node = map.get(t.id)!
    if (t.parent_id === 0 || !map.has(t.parent_id)) {
      roots.push(node)
    } else {
      map.get(t.parent_id)!.children.push(node)
    }
  }
  return roots
})

async function loadSourceKeys(): Promise<void> {
  try {
    const s = await GetAllSources()
    sourceKeys.value = (s as any[]).map((x: any) => x.source_key || '')
  } catch { /* */ }
}

async function loadTypes(): Promise<void> {
  try {
    const types = await GetTypes({ source_key: typeSourceKey.value || '' }) as any[]
    allTypes.value = types.map((t: any) => ({
      id: t.id || 0,
      type_name: t.type_name || '',
      parent_id: t.parent_id || 0,
      sort: t.sort || 0,
      source_key: t.source_key || '',
    }))
  } catch {
    allTypes.value = []
  }
}

onMounted(async () => {
  await loadSourceKeys()
  await loadTypes()
})
</script>

<template>
  <div>
    <div class="a-card">
      <div class="a-card-hd">
        <h3>视频分类 ({{ allTypes.length }})</h3>
        <div class="a-card-hd-acts">
          <select v-model="typeSourceKey" class="a-sel" @change="loadTypes()">
            <option value="">全部源</option>
            <option v-for="sk in sourceKeys" :key="sk" :value="sk">{{ sk }}</option>
          </select>
        </div>
      </div>

      <Empty v-if="allTypes.length === 0" title="暂无分类数据" />
      <table v-else class="a-tb">
        <thead>
          <tr>
            <th style="width:60px">ID</th>
            <th>源</th>
            <th>分类名称</th>
            <th style="width:80px">父分类ID</th>
            <th style="width:60px">排序</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="root in treeTypes" :key="`${root.source_key}-${root.id}`">
            <tr>
              <td class="a-tb-num">{{ root.id }}</td>
              <td class="a-tb-mono">{{ root.source_key }}</td>
              <td class="a-tb-name">{{ root.type_name }}</td>
              <td class="a-tb-num">{{ root.parent_id }}</td>
              <td class="a-tb-num">{{ root.sort }}</td>
            </tr>
            <tr v-for="child in root.children" :key="`${child.source_key}-${child.id}`">
              <td class="a-tb-num">{{ child.id }}</td>
              <td class="a-tb-mono">{{ child.source_key }}</td>
              <td style="padding-left:28px;color:var(--text-secondary)">└ {{ child.type_name }}</td>
              <td class="a-tb-num">{{ child.parent_id }}</td>
              <td class="a-tb-num">{{ child.sort }}</td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>
  </div>
</template>
