import { Marked } from 'marked'
import { markedHighlight } from 'marked-highlight'
import hljs from 'highlight.js/lib/common'
import DOMPurify from 'dompurify'

// Highlighting instance: used once a text section is complete. Syntax
// highlighting a partially-streamed code block is wasted work (the block
// re-highlights on every token), so streaming uses the plain instance below.
const highlighted = new Marked(
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

// Plain instance: no syntax highlighting. Used for the still-streaming section
// of an assistant turn, where the markup keeps changing token by token.
const plain = new Marked({ breaks: true, gfm: true })

/**
 * renderMarkdown converts markdown to sanitised HTML. The sanitisation step is
 * mandatory: message and tool content originates from the model and tools and
 * must never be trusted to be HTML-safe.
 *
 * When `highlight` is true (a completed section) code blocks are syntax
 * highlighted and given a copy button; while streaming, pass false to skip both.
 */
export function renderMarkdown(src: string, highlight = true): string {
  const marked = highlight ? highlighted : plain
  const raw = marked.parse(src ?? '', { async: false }) as string
  const clean = DOMPurify.sanitize(raw, { USE_PROFILES: { html: true } })
  return highlight ? addCopyButtons(clean) : clean
}

/**
 * addCopyButtons wraps each code block in a relative container with a copy
 * button. The button is purely declarative (a `data-copy` marker); the click is
 * handled by event delegation in ChatMessage so it survives re-renders.
 */
function addCopyButtons(html: string): string {
  if (typeof window === 'undefined' || typeof window.DOMParser === 'undefined') return html
  if (!html.includes('<pre')) return html
  const doc = new window.DOMParser().parseFromString(`<body>${html}</body>`, 'text/html')
  for (const pre of Array.from(doc.querySelectorAll('pre'))) {
    const wrapper = doc.createElement('div')
    wrapper.className = 'code-block'
    const btn = doc.createElement('button')
    btn.type = 'button'
    btn.className = 'code-copy'
    btn.setAttribute('data-copy', '')
    btn.setAttribute('aria-label', 'Copy code')
    btn.textContent = 'Copy'
    pre.replaceWith(wrapper)
    wrapper.append(btn, pre)
  }
  return doc.body.innerHTML
}
