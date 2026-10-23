export interface ReadingInputPreferences {wheelPaging:boolean;keyboardPaging:boolean;previousKey:string;nextKey:string}
export function normalizeReaderKey(value:unknown,fallback:string):string {return typeof value==='string'&&/^(Arrow(?:Left|Right|Up|Down)|Key[A-EG-Z]|Digit[0-9])$/.test(value)?value:fallback}
export function normalizeReadingInput(raw:Partial<ReadingInputPreferences>):ReadingInputPreferences {
 const previousKey=normalizeReaderKey(raw.previousKey,'ArrowLeft'),nextKey=normalizeReaderKey(raw.nextKey,'ArrowRight')
 return {wheelPaging:typeof raw.wheelPaging==='boolean'?raw.wheelPaging:true,keyboardPaging:typeof raw.keyboardPaging==='boolean'?raw.keyboardPaging:true,previousKey,nextKey:nextKey===previousKey?(previousKey==='ArrowRight'?'ArrowLeft':'ArrowRight'):nextKey}
}
