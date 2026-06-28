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
}

function readAs(file: File, as: 'text' | 'dataURL'): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result as string)
    reader.onerror = reject
    if (as === 'text') reader.readAsText(file)
    else reader.readAsDataURL(file)
  })
}

export function useAttachments() {
  const attachments = ref<Attachment[]>([])
  const hasAttachments = computed(() => attachments.value.length > 0)

  async function addFile(file: File) {
    let mode: Attachment['mode']
    let content: string

    if (file.type.startsWith('image/')) {
      mode = 'image'
      content = await readAs(file, 'dataURL')
    } else if (isTextLike(file)) {
      mode = 'text'
      content = await readAs(file, 'text')
    } else {
      mode = 'binary'
      content = await readAs(file, 'dataURL')
    }

    attachments.value.push({
      id: `${Date.now()}-${Math.random().toString(36).slice(2)}`,
      name: file.name,
      size: file.size,
      mimeType: file.type,
      content,
      mode,
    })
  }

  function remove(id: string) {
    attachments.value = attachments.value.filter((a) => a.id !== id)
  }

  function clear() {
    attachments.value = []
  }

  function buildPromptSuffix(): string {
    if (!attachments.value.length) return ''
    return (
      '\n\n' +
      attachments.value
        .map((a) => {
          if (a.mode === 'image') return `![${a.name}](${a.content})`
          if (a.mode === 'text') return `\`\`\`${langFromName(a.name)}\n${a.content}\n\`\`\``
          return `\`\`\`binary\n${a.content}\n\`\`\``
        })
        .join('\n\n')
    )
  }

  return { attachments, hasAttachments, addFile, remove, clear, buildPromptSuffix }
}
