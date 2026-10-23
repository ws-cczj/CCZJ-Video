import { test } from 'node:test'
import assert from 'node:assert/strict'
import { checkSource } from './check-architecture.mjs'

test('comments and template text do not create forbidden imports', () => {
  assert.deepEqual(checkSource('// bindings/cczjVideo/app/library/models.js\nconst a = 1', 'frontend/src/comic/domain.ts'), [])
  assert.deepEqual(checkSource('<template>localStorage.foo @wailsio/runtime</template><script setup lang="ts">const a = 1</script>', 'frontend/src/views/Demo.vue'), [])
})
test('static, re-exported and dynamic imports use the transport adapter', () => {
  for (const source of ["import {\nCall\n} from '@wailsio/runtime'", "export * from '../../bindings/cczjVideo/app/service/app'", "import('@wailsio/runtime')"]) {
    assert.equal(checkSource(source, 'frontend/src/stores/a.ts').length, 1)
    assert.equal(checkSource(source, 'frontend/src/api/a.ts').length, source.includes('bindings/') ? 1 : 0)
    assert.deepEqual(checkSource(source, 'frontend/src/api/app.ts'), [])
  }
})
test('browser storage ownership checks executable references', () => {
  for (const source of ['localStorage.getItem("a")', 'window.localStorage.getItem("a")', 'globalThis["localStorage"].getItem("a")']) {
    assert.equal(checkSource(source, 'frontend/src/stores/a.ts').length, 1)
    assert.deepEqual(checkSource(source, 'frontend/src/platform/a.ts'), [])
  }
})
