import { isMobileRuntime } from '../api/runtime'

interface ReadingBarsBridge { readerSystemBars?(owner:string, reading:boolean):boolean }
let sequence=0

/** The reader owns native bars independently of Chromium's video fullscreen. */
export function createReadingSystemBars() {
 const bridge=isMobileRuntime()?(window as unknown as {wails?:ReadingBarsBridge}).wails:undefined
 const owner=`reader-${Date.now()}-${++sequence}`
 let held=false
 return {
  enter():void {held=bridge?.readerSystemBars?.(owner,true)??false},
  release():void {if(held)bridge?.readerSystemBars?.(owner,false);held=false},
 }
}
