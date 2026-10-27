import MarkdownIt from 'markdown-it'

// Release notes are remote Markdown. Keep raw HTML as text and let the parser
// validate link protocols; rendering must never enable upstream HTML scripts.
const markdown = new MarkdownIt({ html: false, linkify: true })
markdown.renderer.rules.link_open = (tokens, index, options, _environment, renderer) => {
  tokens[index]!.attrSet('target', '_blank')
  tokens[index]!.attrSet('rel', 'noopener noreferrer')
  return renderer.renderToken(tokens, index, options)
}

export function renderMarkdown(source: string): string {
  return markdown.render(source)
}
