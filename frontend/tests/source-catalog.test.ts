import { expect, it, vi } from 'vitest'
vi.mock('../src/api/app',()=>({}))
import { compareSources, sourceHealthStatus, sourceStatus, visibleSourceCandidates, type SourceCandidate } from '../src/api/sourceCatalog'

const source=(name:string,status?:'healthy'|'partial'|'failed',checkedAt=1000):SourceCandidate=>({id:name,name,apiUrl:'https://example.org',kind:'novel',project:'Legado',origin:'',format:'adapted',compatible:true,reason:'',summary:'',imported:false,...(status?{quality:{status,checkedAt,score:status==='healthy'?100:20,millis:200,samples:1,successes:status==='healthy'?1:0,stages:[]}}:{})})
it('ranks complete reading above partial checks and leaves untested and expired evidence explicit',()=>{
 const rows=[source('untested'),source('failed','failed'),source('partial','partial'),source('good','healthy'),{...source('unsupported'),compatible:false}]
 rows.sort((a,b)=>compareSources(a,b,'quality',1000*1000))
 expect(rows.map(row=>row.name)).toEqual(['good','partial','untested','failed','unsupported'])
 expect(sourceStatus(source('old','healthy'),1000*1000+25*3600*1000)).toBe('stale')
 expect(sourceStatus(source('new'))).toBe('untested')
})

it('uses the same stale health classification on installed sources and hides incompatible bundled candidates', () => {
  expect(sourceHealthStatus(true, { status: 'healthy', checkedAt: 1000 }, 1000 * 1000 + 25 * 3600 * 1000)).toBe('stale')
  expect(sourceHealthStatus(false, { status: 'healthy', checkedAt: 1000 }, 1000 * 1000)).toBe('unsupported')
  const rows = [source('good', 'healthy'), { ...source('bad', 'failed'), compatible: false }]
  expect(visibleSourceCandidates(rows, 'bundled').map(row => row.name)).toEqual(['good'])
  expect(visibleSourceCandidates(rows, 'community').map(row => row.name)).toEqual(['good', 'bad'])
})

it('prefers current multi-work evidence over an old first-chapter score and expires both',()=>{
 const quick=source('quick','healthy')
 const benchmark=source('benchmark','partial')
 benchmark.quality={...benchmark.quality!,score:70,evidence:'benchmark:reading-quality.v1',samples:9,successes:6}
 expect([quick,benchmark].sort((a,b)=>compareSources(a,b,'quality',1000*1000)).map(row=>row.name)).toEqual(['benchmark','quick'])
 benchmark.quality.checkedAt=1
 expect([quick,benchmark].sort((a,b)=>compareSources(a,b,'quality',1000*1000+24*3600*1000-1)).map(row=>row.name)).toEqual(['quick','benchmark'])
})
