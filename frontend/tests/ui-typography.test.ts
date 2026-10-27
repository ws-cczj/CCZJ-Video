import { afterEach, expect, it, vi } from 'vitest'
import { createSSRApp } from 'vue'
import { renderToString } from '@vue/server-renderer'
import Empty from '../src/components/ui/Empty.vue'
import Segment from '../src/components/ui/Segment.vue'

const settings=vi.hoisted(()=>({read:vi.fn(async (_key:string)=>'')}))
vi.mock('../src/api/app',()=>({GetSetting:settings.read}))
import { applyFontSize, loadTypography } from '../src/platform/typography'

afterEach(()=>vi.unstubAllGlobals())

it('restores saved interface typography before rendering and handles unknown size keys',async()=>{
  const style={fontSize:'',fontFamily:'',removeProperty:vi.fn()}
  vi.stubGlobal('document',{documentElement:{style}})
  settings.read.mockImplementation(async key=>key==='font_size_key'?'xxl':'KaiTi, serif')
  await loadTypography()
  expect(style.fontSize).toBe('20px')
  expect(style.fontFamily).toBe('KaiTi, serif')
  applyFontSize('unrecognised-old-value')
  expect(style.fontSize).toBe('14px')
})

it('shows a repeated error once while keeping a distinct recovery hint',async()=>{
  const repeated=await renderToString(createSSRApp(Empty,{title:'源站搜索失败',description:' 源站搜索失败 '}))
  expect(repeated.match(/源站搜索失败/g)).toHaveLength(1)
  const distinct=await renderToString(createSSRApp(Empty,{title:'源站搜索失败',description:'请稍后重试'}))
  expect(distinct).toContain('请稍后重试')
})

it('keeps every wrapped segment choice and active state without a row-specific indicator',async()=>{
  const rendered=await renderToString(createSSRApp(Segment,{modelValue:'storage',wrap:true,options:[{value:'overview',label:'资源概览'},{value:'storage',label:'运行环境与存储'}]}))
  expect(rendered.match(/<button/g)).toHaveLength(2)
  expect(rendered).toContain('运行环境与存储')
  expect(rendered).toContain('aria-pressed="true"')
  expect(rendered).not.toContain('class="ui-seg__indicator"')
})
