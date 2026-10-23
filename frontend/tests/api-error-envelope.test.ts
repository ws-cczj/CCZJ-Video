import { expect, it, vi } from 'vitest'
import { normalizeApiError } from '../src/api/app'
it.each([
 ['Binding call failed: Bound method returned an error: UNAVAILABLE: Service unavailable', {code:'UNAVAILABLE',message:'Service unavailable'}],
 ['Bound method returned an error: STORAGE: Disk full', {code:'STORAGE',message:'Disk full'}],
 ['CONFLICT: Already exists', {code:'CONFLICT',message:'Already exists'}],
 ['catalog lookup: no such row', {code:'INTERNAL',message:'操作失败，请查看日志'}],
])('normalizes a service error without exposing its Wails envelope: %s', (message, expected) => {
 expect(normalizeApiError(new Error(message))).toEqual(expected)
})

it('reads a structured Wails cause independently of its diagnostic message', () => {
  const error = new Error('framework wording may change')
  Object.assign(error, { cause: { code: 'STORAGE', message: '保存失败', cause: 'private path' } })
  expect(normalizeApiError(error)).toEqual({ code: 'STORAGE', message: '保存失败' })
})

it('preserves structured retry metadata through a runtime envelope', () => {
  expect(normalizeApiError({ cause: { cause: [{ code: 'RATE_LIMITED', message: '稍后重试', retryAfterSeconds: 20 }] } }))
    .toEqual({ code: 'RATE_LIMITED', message: '稍后重试', retryAfterSeconds: 20 })
})

it('handles runtime cancellation and cyclic malformed causes', () => {
  const error = new Error('cancelled')
  error.name = 'CancelError'
  expect(normalizeApiError(error).code).toBe('CANCELLED')
  const cyclic: { cause?: unknown } = {}
  cyclic.cause = cyclic
  expect(normalizeApiError(cyclic).code).toBe('INTERNAL')
})

it('accepts numeric legacy envelopes and preserves correlation without diagnostic leakage', () => {
 expect(normalizeApiError({cause:{code:429,msg:'稍后重试',requestId:'r1',operation:'danmaku.search',logged:true,retryAfterSeconds:30,stack:'private'}})).toEqual({code:'RATE_LIMITED',message:'稍后重试',requestId:'r1',operation:'danmaku.search',logged:true,retryAfterSeconds:30})
})
