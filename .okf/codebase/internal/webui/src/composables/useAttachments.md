---
description: Source module internal/webui/src/composables/useAttachments.ts (95 lines).
resource: internal/webui/src/composables/useAttachments.ts
tags:
    - ts
    - source
timestamp: "2026-07-21T18:36:12Z"
title: useAttachments.ts
type: Module
---

# Module useAttachments.ts

**Path**: `internal/webui/src/composables/useAttachments.ts`  
**Lines**: 95

## Snippet Preview

```
import { computed, ref } from 'vue'

export interface Attachment {
  id: string
  name: string
  size: number
  mimeType: string
  content: string
  mode: 'text' | 'image' | 'binary'
}

const TEXT_EXTENSIONS = new Set(['json', 'yaml', 'yml', 'xml', 'toml', 'csv', 'md', 'txt'])

function isTextLike(file: File): boolean {
  if (file.type.startsWith('text/')) return true
  const ext = file.name.split('.').pop()?.toLowerCase() ?? ''
  return TEXT_EXTENSIONS.has(ext)
}

function langFromName(name: string): string {
  const ext = name.split('.').pop()?.toLowerCase() ?? ''
  const map: Record<string, string> = {
    ts: 'ts', tsx: 'tsx', js: 'js', jsx: 'jsx', vue: 'vue',
    py: 'python', go: 'go', rs: 'rust', java: 'java', kt: 'kotlin',
    swift: 'swift', rb: 'ruby', php: 'php', cs: 'csharp',
    cpp: 'cpp', c: 'c', h: 'c', sh: 'bash', bash: 'bash',
    json: 'json', yaml: 'yaml', yml: 'yaml', xml: 'xml',
    toml: 'toml', md: 'markdown', html: 'html', css: 'css', sql: 'sql',
  }
  return map[ext] ?? ext
```
