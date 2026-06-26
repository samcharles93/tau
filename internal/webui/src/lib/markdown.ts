import { Marked } from 'marked'
import { markedHighlight } from 'marked-highlight'
import hljs from 'highlight.js/lib/common'
import DOMPurify from 'dompurify'

// A dedicated Marked instance with synchronous syntax highlighting. Synchronous
// is important: assistant text streams token-by-token and we re-render often.
const marked = new Marked(
  markedHighlight({
    emptyLangClass: 'hljs',
    langPrefix: 'hljs language-',
    highlight(code, lang) {
      const language = lang && hljs.getLanguage(lang) ? lang : 'plaintext'
      return hljs.highlight(code, { language }).value
    },
  }),
  { breaks: true, gfm: true },
)

/**
 * renderMarkdown converts markdown to sanitised HTML. The sanitisation step is
 * mandatory: message and tool content originates from the model and tools and
 * must never be trusted to be HTML-safe.
 */
export function renderMarkdown(src: string): string {
  const raw = marked.parse(src ?? '', { async: false }) as string
  return DOMPurify.sanitize(raw, { USE_PROFILES: { html: true } })
}
