import { expect, it, vi } from 'vitest'
import { decodeNetworkPolicy, networkRetryDelay } from '../src/network/requestPolicy'

it('defaults to the standard global guard and rejects invalid persisted policy',()=>{
 expect(decodeNetworkPolicy('')).toEqual({frequency:'standard'})
 expect(decodeNetworkPolicy('{"frequency":"steady"}')).toEqual({frequency:'steady'})
 for(const raw of ['null','[]','{"frequency":null}','{"frequency":"fast"}'])expect(()=>decodeNetworkPolicy(raw)).toThrow()
})

it('uses danmaku fallback jitter while preserving the exact remaining backend cooldown',()=>{
 const random=vi.spyOn(Math,'random').mockReturnValue(0)
 expect(networkRetryDelay('standard')).toBe(300000);expect(networkRetryDelay('steady')).toBe(600000);expect(networkRetryDelay('low')).toBe(1800000)
 random.mockReturnValue(.99999);expect(networkRetryDelay('standard')).toBe(360000)
 expect(networkRetryDelay('standard',30)).toBe(30000);expect(networkRetryDelay('standard',1800)).toBe(1800000)
 random.mockRestore()
})
