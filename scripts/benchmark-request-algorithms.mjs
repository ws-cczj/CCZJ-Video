import { readFileSync, writeFileSync } from 'node:fs'
import { performance } from 'node:perf_hooks'
import { fileURLToPath } from 'node:url'

const root = new URL('../', import.meta.url)
const reader = readFileSync(new URL('frontend/src/novel/views/NovelReader.vue', root), 'utf8')
if (!reader.includes("chapter?.id===entry.chapter.id?'page':undefined")) throw new Error('Update the benchmark to match the current directory expression')
const median = values => values.toSorted((a,b) => a-b)[Math.floor(values.length/2)]
const results = []
for (const count of [1000, 5000, 10000]) {
  const chapters = Array.from({length:count},(_,index)=>({id:index+100}))
  const currentIndex = Math.floor(count*.75), current = chapters[currentIndex]
  const before = () => chapters.map(entry=>currentIndex===chapters.findIndex(chapter=>chapter.id===entry.id))
  const after = () => chapters.map(entry=>current.id===entry.id)
  if (JSON.stringify(before())!==JSON.stringify(after())) throw new Error('Active chapter changed')
  function measure(work) {
    work(); work()
    return median(Array.from({length:9},()=>{const began=performance.now();work();return performance.now()-began}))
  }
  results.push({chapters:count,beforeComparisons:count*(count+1)/2,afterComparisons:count,beforeMedianMS:measure(before),afterMedianMS:measure(after)})
}
const result = {scope:'directory current-chapter expression only; synthetic chapter metadata; excludes DOM layout and network',node:process.version,results}
const target = new URL('reports/request-pipelines-2026-10-20/directory-benchmarks.json',root)
writeFileSync(target, JSON.stringify(result,null,2)+'\n')
console.log(fileURLToPath(target)); console.log(JSON.stringify(result))
