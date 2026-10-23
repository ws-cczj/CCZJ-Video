import { afterEach, expect, it, vi } from 'vitest'
vi.mock('../src/locales',()=>({tr:(key:string)=>key}))
import { openAndroidUpdateInstaller } from '../src/platform/updateInstaller'
afterEach(()=>vi.unstubAllGlobals())
it('reports an older Android host without claiming installation',()=>{
  vi.stubGlobal('window',{wails:{}})
  expect(()=>openAndroidUpdateInstaller('/private/update.apk')).toThrow('update.androidHostRequired')
})
it('preserves system permission and package validation errors',()=>{
  vi.stubGlobal('window',{wails:{installApk:()=> 'Signing certificate mismatch'}})
  expect(()=>openAndroidUpdateInstaller('/private/update.apk')).toThrow('Signing certificate mismatch')
})
it('hands the validated path to the system installer bridge',()=>{
  const installApk=vi.fn(()=> '')
  vi.stubGlobal('window',{wails:{installApk}})
  openAndroidUpdateInstaller('/private/update.apk')
  expect(installApk).toHaveBeenCalledWith('/private/update.apk')
})
