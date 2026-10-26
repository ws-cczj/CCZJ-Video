import { primaryTabs } from './routeMotion'

/** A finite atlas of viewport positions, independent of history and item IDs.
 * Reopening a route or cancelling a swipe returns to the identical crop.
 */
export function routeBackdropFocus(path:string,group=''): {x:number;y:number} {
 const tabs=primaryTabs.find(rows=>rows.includes(path))
 if(tabs)return {x:tabs.indexOf(path)/Math.max(1,tabs.length-1),y:group ? 1:.06}
 const mode=path.startsWith('/novel/')?'novel':path.startsWith('/comic/')?'comic':'video'
 if(/\/(read|player)\//.test(path))return {x:.5,y:.5}
 if(/\/(book|series|detail)\//.test(path))return {x:.25,y:.6}
 if(/\/(search|categories)$/.test(path))return {x:.35,y:.4}
 return {x:mode==='video'?.85:1,y:.75}
}

/** Main tabs occupy equal viewport-sized cells of one image; subpages use row 2. */
export function routeBackdropFrame(path:string,focus:{x:number;y:number}):Record<string,string> {
 const modeRoot=path.startsWith('/novel')?'/novel':path.startsWith('/comic')?'/comic':'/'
 const columns=(primaryTabs.find(tabs=>tabs[0]===modeRoot)||primaryTabs[0]).length
 return {width:`${columns*100}%`,height:'200%',transform:`translate3d(${-focus.x*(columns-1)/columns*100}%,${-focus.y*50}%,0)`}
}
