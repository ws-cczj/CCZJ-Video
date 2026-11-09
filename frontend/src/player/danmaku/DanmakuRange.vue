<script setup lang="ts">
import { computed, useId } from 'vue'
const props=defineProps<{label:string;value:number;min:number;max:number;step:number;display:string}>()
const emit=defineEmits<{change:[value:number]}>()
const id=useId()
const percent=computed(()=>Math.max(0,Math.min(100,(props.value-props.min)/(props.max-props.min)*100)))
</script>
<template>
 <div class="cczj-danmaku-range">
  <label :for="id">{{label}}</label>
  <input :id="id" type="range" :value="value" :min="min" :max="max" :step="step" :aria-valuetext="display" :style="{'--dm-fill':percent+'%'}" @input="emit('change',Number(($event.target as HTMLInputElement).value))" />
  <output :for="id" :title="String(value)">{{display}}</output>
 </div>
</template>
<style scoped>
.cczj-danmaku-range{@apply cczj-grid cczj-items-center cczj-gap-4;grid-template-columns:64px minmax(40px,1fr) 46px;min-height:36px}
label,output{font-size:calc(12rem / 14);font-variant-numeric:tabular-nums}
output{@apply cczj-text-right;color:var(--text-secondary)}
input{@apply cczj-w-full cczj-min-w-0 cczj-cursor-pointer;appearance:none;margin:0;height:28px;background:transparent;touch-action:pan-y;--dm-rail:#ffffff30}
input::-webkit-slider-runnable-track{height:4px;border-radius:4px;background:linear-gradient(to right,var(--accent) var(--dm-fill),var(--dm-rail) var(--dm-fill))}
input::-webkit-slider-thumb{appearance:none;width:13px;height:13px;margin-top:-4.5px;border-radius:50%;border:0;background:#fff;box-shadow:0 1px 4px #0005}
input::-moz-range-track{height:4px;border-radius:4px;background:var(--dm-rail)}
input::-moz-range-progress{height:4px;border-radius:4px;background:var(--accent)}
input::-moz-range-thumb{width:13px;height:13px;border:0;border-radius:50%;background:#fff}
input:focus-visible{outline:2px solid var(--accent);outline-offset:2px;border-radius:4px}
:global(.cczj-danmaku-touch) .cczj-danmaku-range{min-height:44px}
:global(.cczj-danmaku-touch) input{height:44px}
</style>
