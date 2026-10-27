import { expect, it } from 'vitest'
import { renderMarkdown } from '../src/utils/markdown'

it('renders release headings, nested lists, emphasis, code, tables and links', () => {
  const html = renderMarkdown('## 修复\n\n- **更新**支持 `Markdown`\n  - 采集提示\n\n> 保留数据\n\n```text\n<a>\n```\n\n| 版本 | 状态 |\n| --- | --- |\n| 2.4.5 | 修复 |\n\n[发布页](https://github.com/ws-cczj/CCZJ-Video/releases)')
  expect(html).toContain('<h2>修复</h2>')
  expect(html).toContain('<strong>更新</strong>支持 <code>Markdown</code>')
  expect(html.match(/<ul>/g)).toHaveLength(2)
  expect(html).toContain('<blockquote>')
  expect(html).toContain('&lt;a&gt;')
  expect(html).toContain('<table>')
  expect(html).toContain('rel="noopener noreferrer"')
})

it('treats upstream HTML as text and rejects executable link protocols', () => {
  const html = renderMarkdown('<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>\n\n[bad](javascript:alert%281%29)\n\n[local](file:///private/data)')
  expect(html).not.toContain('<script>')
  expect(html).not.toContain('<img')
  expect(html).not.toContain('href="javascript:')
  expect(html).not.toContain('href="file:')
  expect(html).toContain('&lt;script&gt;')
})
