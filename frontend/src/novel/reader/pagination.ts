export function pageFromRatio(ratio:number,count:number):number { return Math.round(Math.max(0,Math.min(1,Number.isFinite(ratio)?ratio:0))*Math.max(0,count-1)) }
export function ratioFromPage(page:number,count:number):number { return count>1 ? Math.max(0,Math.min(count-1,page))/(count-1) : 0 }
export function measuredPageCount(scrollWidth:number,width:number,gap:number):number { return width>0 ? Math.max(1,Math.ceil((scrollWidth+gap-1)/(width+gap))) : 1 }
