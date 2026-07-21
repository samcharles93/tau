---
description: Source module internal/webui/src/lib/markdown.ts (62 lines).
resource: internal/webui/src/lib/markdown.ts
tags:
    - ts
    - source
timestamp: "2026-07-21T18:36:12Z"
title: markdown.ts
type: Module
---

# Module markdown.ts

**Path**: `internal/webui/src/lib/markdown.ts`  
**Lines**: 62

## Snippet Preview

```
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
```
