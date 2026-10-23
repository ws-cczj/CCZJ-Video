import { expect,it,vi } from 'vitest'
import { createSSRApp } from 'vue'
import { createPinia } from 'pinia'
import { createMemoryHistory,createRouter } from 'vue-router'
import { renderToString } from '@vue/server-renderer'
import { createI18n } from 'vue-i18n'

vi.mock('../src/locales',()=>({tr:(key:string)=>key,setLocale:async()=>{}}))
vi.mock('../src/router',()=>({default:{}}))
vi.mock('../src/stores/motion',()=>({useMotionStore:()=>({enabled:true,options:{},duration:()=>0,load:async()=>{}})}))
vi.mock('../src/api/runtime',async original=>({...await original<object>(),isMobileRuntime:()=>false}))
import Settings from '../src/views/Settings.vue'

it('renders settings on first entry without losing the route tree',async()=>{
 const router=createRouter({history:createMemoryHistory(),routes:[{path:'/settings',component:Settings}]})
 await router.push('/settings');await router.isReady()
 const app=createSSRApp(Settings).use(createPinia()).use(router).use(createI18n({legacy:false,locale:'zh',missingWarn:false,fallbackWarn:false,messages:{zh:{}}}))
 const errors:unknown[]=[];app.config.errorHandler=error=>{errors.push(error)}
 const html=await renderToString(app)
 expect(errors.map(error=>error instanceof Error?error.stack:error)).toEqual([])
 expect(html).toContain('settings-page')
 expect(html).toContain('settings-content')
})
