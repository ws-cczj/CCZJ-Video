<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { GetSetting, SetSetting, normalizeApiError } from '../api/app'
import { NETWORK_POLICY_KEY, networkFrequencies, decodeNetworkPolicy } from '../network/requestPolicy'
import { Button, Select } from './ui'
const { t } = useI18n()
const frequencies=computed(()=>networkFrequencies.map(value=>({value,label:t(`networkPolicy.frequencies.${value}`)})))
const settings=ref(decodeNetworkPolicy('')),ready=ref(false),saving=ref(false),saved=ref(false),error=ref('')
let disposed=false
async function load(){error.value='';try{const raw=await GetSetting(NETWORK_POLICY_KEY);if(!disposed){settings.value=decodeNetworkPolicy(raw);ready.value=true}}catch(value){if(!disposed)error.value=normalizeApiError(value).message}}
async function save(){if(!ready.value||saving.value)return;saving.value=true;error.value='';saved.value=false;try{await SetSetting(NETWORK_POLICY_KEY,JSON.stringify(settings.value));if(!disposed)saved.value=true}catch(value){if(!disposed)error.value=normalizeApiError(value).message}finally{if(!disposed)saving.value=false}}
onMounted(load);onBeforeUnmount(()=>{disposed=true})
</script>
<template>
 <section class="block cczj-network-policy">
  <h2>{{t('networkPolicy.title')}}</h2><p>{{t('networkPolicy.description')}}</p>
  <p v-if="error" role="alert">{{error}} <Button v-if="!ready" variant="ghost" @click="load">{{t('common.retry')}}</Button></p>
  <div class="cczj-network-policy-controls" :aria-busy="saving"><label for="global-network-frequency">{{t('networkPolicy.frequency')}}</label><Select id="global-network-frequency" v-model="settings.frequency" :options="frequencies" :ariaLabel="t('networkPolicy.frequency')" :disabled="!ready||saving" @change="saved=false" /><Button :disabled="!ready||saving" @click="save">{{saving?t('common.loading'):t('common.save')}}</Button><span role="status">{{saved?t('networkPolicy.saved'):!ready&&!error?t('common.loading'):''}}</span></div>
  <details><summary>{{t('networkPolicy.details')}}</summary><p>{{t('networkPolicy.queryHint')}}</p><p>{{t('networkPolicy.streamHint')}}</p><p>{{t('networkPolicy.persistenceHint')}}</p><p>{{t('networkPolicy.proxyHint')}}</p></details>
 </section>
</template>
<style scoped>
.cczj-network-policy{@apply cczj-text-primary cczj-min-w-0}.cczj-network-policy h2{font-size:1.1rem;margin:0 0 10px}.cczj-network-policy p{@apply cczj-text-13;line-height:1.7;@apply cczj-text-secondary;margin:8px 0}.cczj-network-policy-controls{@apply cczj-flex cczj-items-center cczj-gap-6 cczj-flex-wrap;padding:12px 0}.cczj-network-policy-controls label{@apply cczj-font-semibold}.cczj-network-policy-controls span{@apply cczj-text-13 cczj-text-secondary}
.cczj-network-policy summary{min-height:36px;@apply cczj-cursor-pointer cczj-text-secondary cczj-text-13;}.cczj-network-policy details{margin-top:8px;}
</style>
