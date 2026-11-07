import { hueOf, readRgb } from '../utils/colors'
export const SPLASH_IMAGE_KEYS=['green','blue','deepblue','skyblue','purple','bitpurple','pink','red','grey','yellow'] as const
export type SplashImageKey=typeof SPLASH_IMAGE_KEYS[number]
export function readSplashKey(value:unknown):SplashImageKey|undefined {
  return typeof value==='string'&&(SPLASH_IMAGE_KEYS as readonly string[]).includes(value)?value as SplashImageKey:undefined
}
const presetKeys:Record<string,SplashImageKey>={
  blue:'blue',sky:'blue',indigo:'blue','d-blue':'blue','d-cyan':'skyblue',cyan:'skyblue',
  deepblue:'deepblue',skyblue:'skyblue',bitblue:'blue',naruto:'blue',
  green:'green','d-green':'green',lime:'green',teal:'green',bitgreen:'green',
  purple:'purple','d-violet':'purple','d-indigo':'purple',bitpurple:'bitpurple',
  pink:'pink','d-rose':'pink',red:'red',orange:'red',happy_new_year:'red',
  minimal:'grey',gray:'grey',grey:'grey',china_ink:'grey',black:'grey',yellow:'yellow',mid_autumn:'bitpurple',
}
/** Persist a stable asset key; hashed asset URLs belong to the current build. */
export function selectSplashKey(id:string,accent:string,custom=false):SplashImageKey {
  if(!custom&&presetKeys[id])return presetKeys[id]
  const channels=readRgb(accent), maximum=Math.max(...channels),minimum=Math.min(...channels)
  if(!channels.every(Number.isFinite)||maximum-minimum<18||(maximum-minimum)/Math.max(1,maximum)<.12)return 'grey'
  const hue=hueOf(accent)
  if(hue<35||hue>=345)return 'red'
  if(hue<75)return 'yellow'
  if(hue<175)return 'green'
  if(hue<205)return 'skyblue'
  if(hue<240)return maximum<140?'deepblue':'blue'
  if(hue<290)return maximum<140?'bitpurple':'purple'
  return 'pink'
}
