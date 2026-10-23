import { ApiFailure } from './errors'

function invalid(): never { throw new ApiFailure({code:'CORRUPT',message:'返回数据格式无效'},undefined) }
export function object(value:unknown):Record<string,unknown>{if(!value||typeof value!=='object'||Array.isArray(value))return invalid();return value as Record<string,unknown>}
export function text(value:unknown):string{if(typeof value!=='string')return invalid();return value}
export function number(value:unknown,min=0,max=Number.MAX_SAFE_INTEGER):number{if(typeof value!=='number'||!Number.isFinite(value)||value<min||value>max)return invalid();return value}
export function integer(value:unknown,min=0,max=Number.MAX_SAFE_INTEGER):number{const result=number(value,min,max);if(!Number.isSafeInteger(result))return invalid();return result}
export function boolean(value:unknown):boolean{if(typeof value!=='boolean')return invalid();return value}
/** Null collection compatibility is explicit; positional nulls need their own decoder. */
export function list<T>(value:unknown,decode:(entry:unknown)=>T,maximum=100000):T[]{if(value==null)return [];if(!Array.isArray(value)||value.length>maximum)return invalid();return value.map(decode)}
export function required<T>(value:T|null|undefined):T{if(value==null)return invalid();return value}
export function each(value:unknown,visit:(entry:unknown)=>unknown,maximum=100000):void{if(value==null)return;if(!Array.isArray(value)||value.length>maximum)return invalid();for(const entry of value)visit(entry)}
export function choice<T extends string>(value:unknown,choices:readonly T[]):T{if(typeof value!=='string'||!choices.some(item=>item===value))return invalid();return value as T}
