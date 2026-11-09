import { InstalledResources, InstallResource, RemoveResource } from './app'
import { object, text, integer, boolean, list } from './decode'
import { response } from './response'
export interface InstalledResource { id:string;version:string;size:number;installed:boolean;corrupt:boolean;url:string }
function entry(value:unknown):InstalledResource {
 const row=object(value),id=text(row.id),version=text(row.version),url=text(row.url)
 if(!/^[a-z0-9-]{1,64}$/.test(id)||!/^[a-z0-9-]{1,80}$/.test(version))throw new Error('Invalid resource identity')
 const installed=boolean(row.installed)
 if(installed&&!url)throw new Error('Missing resource URL')
 if(url&&(url!==`/__cczj/resource/${id}/${version}/font.woff2`||!installed))throw new Error('Invalid resource URL')
 return {id,version,size:integer(row.size,1,64*1024*1024),installed,corrupt:boolean(row.corrupt),url}
}
export const installedResourcesAPI={
 list:()=>response(()=>InstalledResources(),value=>{const row=object(value);return {directory:text(row.directory),items:list(row.items,entry,16)}}),
 install:(id:string)=>response(()=>InstallResource(id),entry),
 remove:(id:string)=>response(()=>RemoveResource(id),()=>undefined),
}
