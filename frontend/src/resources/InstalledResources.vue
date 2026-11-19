<script setup lang="ts">
import { onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '../components/ui'
import { useInstalledResources } from './store'
const store=useInstalledResources(),{t}=useI18n()
onMounted(()=>{void store.load()})
</script>
<template>
 <section v-if="store.items.length || store.error" class="installed-resources cczj-grid cczj-gap-4 cczj-min-w-0" :aria-label="t('resources.title')">
  <h4>{{t('resources.title')}}</h4>
  <p v-if="store.directory" class="resource-directory">{{t('resources.directory')}}: {{store.directory}}</p>
  <div v-for="item in store.items" :key="item.id" class="resource-item cczj-flex cczj-flex-wrap cczj-items-center cczj-justify-between cczj-gap-4">
   <div class="cczj-min-w-0"><strong>{{t('resources.sourceHan')}}</strong><p>{{item.corrupt?t('resources.corrupt'):`${(item.size/1024/1024).toFixed(2)} MiB`}}</p></div>
   <div class="cczj-flex cczj-flex-wrap cczj-gap-3">
    <Button v-if="store.busy===item.id" variant="text" @click="store.cancel">{{t('common.cancel')}}</Button>
    <Button v-else variant="text" :disabled="!!store.busy" @click="store.remove(item.id)">{{t('resources.remove')}}</Button>
   </div>
  </div>
  <p v-if="store.busy" role="status">{{t('resources.removing')}}</p>
  <p v-if="store.error" role="alert">{{store.error}} <Button variant="text" :disabled="!!store.busy" @click="store.load">{{t('common.refresh')}}</Button></p>
 </section>
</template>
<style scoped>
.installed-resources{margin:12px 0;font-size:calc(12rem / 14)}
h4,p{margin:0}
p{color:var(--text-secondary);line-height:1.6}
.resource-directory{overflow-wrap:anywhere;user-select:text}
.resource-item{padding:8px 0}
strong{font-weight:500}
</style>
