<script setup lang="ts">
import { onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '../components/ui'
import { useInstalledResources } from './store'
defineProps<{compact?:boolean}>()
const store=useInstalledResources(),{t}=useI18n()
onMounted(()=>{void store.load()})
</script>
<template>
 <section class="installed-resources cczj-grid cczj-gap-4 cczj-min-w-0" :aria-label="t('resources.title')">
  <template v-if="!compact"><h4>{{t('resources.title')}}</h4><p>{{t('resources.hint')}}</p><p v-if="store.directory" class="resource-directory">{{t('resources.directory')}}: {{store.directory}}</p></template>
  <div v-for="item in store.items" :key="item.id" class="resource-item cczj-flex cczj-flex-wrap cczj-items-center cczj-justify-between cczj-gap-4">
   <div class="cczj-min-w-0"><strong>{{t('resources.sourceHan')}}</strong><p>{{(item.size/1024/1024).toFixed(2)}} MiB · {{t(item.corrupt?'resources.corrupt':item.installed?'resources.installed':'resources.notInstalled')}}</p></div>
   <div class="cczj-flex cczj-flex-wrap cczj-gap-3">
    <Button v-if="store.busy===item.id" variant="text" @click="store.cancel">{{t('common.cancel')}}</Button>
    <Button v-else-if="item.installed" variant="text" :disabled="!!store.busy" @click="store.change(item.id,true)">{{t('resources.remove')}}</Button>
    <Button v-else variant="secondary" :disabled="!!store.busy" @click="store.change(item.id)">{{t('resources.install')}}</Button>
   </div>
  </div>
  <p v-if="store.busy" role="status">{{t('resources.installing')}}</p>
  <p v-if="store.error" role="alert">{{store.error}} <Button variant="text" :disabled="!!store.busy" @click="store.load">{{t('common.refresh')}}</Button></p>
  <Button v-if="!store.ready&&!store.error" variant="text" @click="store.load">{{t('common.refresh')}}</Button>
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
