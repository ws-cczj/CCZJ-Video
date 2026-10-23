<script setup lang="ts">
import { reactive } from 'vue'
import { Button, Badge, Tag, Empty, Spinner, Modal, NumberField, Select, Segment } from '../../src/components/ui'
import { useThemeStore } from '../../src/stores/theme'

const state=reactive({mode:'video',modal:false,customModal:false,loading:false,number:2,selected:'one',segment:'one',confirmed:0})
const options=[{value:'one',label:'第一个选项'},{value:'two',label:'第二个选项'}]
Object.assign(window,{styleLibrary:{state,theme:useThemeStore()}})
</script>
<template>
  <main class="style-fixture" :class="{'cczj-novel-shell':state.mode==='novel','cczj-comic-shell':state.mode==='comic'}">
    <h1>CCZJ 样式复用验收</h1>
    <div class="fixture-row">
      <div data-probe="direct" class="cczj-flex cczj-items-center cczj-gap-4 cczj-rounded-md cczj-border cczj-text-primary">共享类</div>
      <div data-probe="composed" class="composed-probe">作用域组合</div>
    </div>
    <div class="fixture-row">
      <Button data-test="primary" variant="primary">确定</Button><Button>取消</Button><Button variant="ghost">更多</Button><Button disabled>不可用</Button><Button :loading="true">正在保存</Button>
    </div>
    <Button block data-test="block">整行按钮</Button>
    <div class="fixture-row"><Badge variant="success">已通过</Badge><Tag closable>内容标签</Tag><NumberField v-model="state.number" :min="0" :max="10" aria-label="数量" /></div>
    <div class="fixture-row"><Select v-model="state.selected" :options="options" /><Segment v-model="state.segment" :options="options" /></div>
    <Button data-test="open-modal" @click="state.modal=true">打开弹窗</Button>
    <Button data-test="open-custom-modal" @click="state.customModal=true">自定义页脚</Button>
    <Spinner size="sm" label="加载内容" />
    <Empty title="暂无内容" description="添加内容后会显示在这里" />
    <Modal v-model="state.modal" title="操作确认" show-footer :ok-loading="state.loading" @ok="state.confirmed++"><p>公共弹窗应复用公共按钮，并保持相同的加载与禁用行为。</p></Modal>
    <Modal v-model="state.customModal" title="自定义操作"><p>调用方提供保存按钮。</p><template #footer><Button @click="state.customModal=false">保存配置</Button></template></Modal>
  </main>
</template>
<style scoped>
.style-fixture{height:auto;min-height:100vh;overflow:visible;padding:20px;box-sizing:border-box;display:flex;flex-direction:column;gap:16px;background:var(--bg-primary);color:var(--text-primary)}
.style-fixture h1{font-size:18px;margin:0}.fixture-row{display:flex;align-items:center;flex-wrap:wrap;gap:12px}
.composed-probe{@apply cczj-flex cczj-items-center cczj-gap-4 cczj-rounded-md cczj-border cczj-text-primary}
</style>
