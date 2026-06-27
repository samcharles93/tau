<template>
  <div
    class="group/msg flex flex-col gap-1.5"
    :class="message.role === 'user' ? 'items-end' : 'items-start'"
  >
    <!-- Ordered timeline: reasoning, text, and tool blocks render in the exact
         order they arrived so the turn reads chronologically. -->
    <template v-for="block in blocks" :key="block.id">
      <!-- Reasoning -->
      <div v-if="block.kind === 'reasoning'" class="w-full max-w-[85%]">
        <ReasoningPanel :reasoning="block.text" :streaming="block.streaming" />
      </div>

      <!-- Text bubble -->
      <div
        v-else-if="block.kind === 'text'"
        class="max-w-[85%] rounded-lg px-3.5 py-2.5 text-sm"
        :class="
          message.role === 'user'
            ? 'bg-primary text-primary-foreground'
            : 'bg-card text-card-foreground border border-border'
        "
      >
        <div
          v-if="message.role === 'assistant'"
          class="prose-chat"
          v-html="render(block.text, block.final)"
          @click="onCopyCode"
        />
        <div v-else class="whitespace-pre-wrap">{{ block.text }}</div>
      </div>

      <!-- Tool calls (consecutive parallel calls grouped) -->
      <div v-else-if="block.kind === 'tools'" class="w-full max-w-[85%]">
        <ToolGroup :tools="block.tools" />
      </div>
    </template>

    <!-- Empty assistant turn placeholder while waiting for the first token. -->
    <div
      v-if="message.role === 'assistant' && message.streaming && !blocks.length"
      class="max-w-[85%] rounded-lg border border-border bg-card px-3.5 py-2.5 text-sm text-muted-foreground"
    >
      …
    </div>

    <!-- Message actions -->
    <div
      v-if="!message.streaming && text"
      class="flex items-center gap-1 opacity-0 transition-opacity group-hover/msg:opacity-100"
    >
      <button
        type="button"
        class="flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] text-muted-foreground hover:bg-muted hover:text-foreground"
        :title="copied ? 'Copied!' : 'Copy message'"
        @click="copyMessage"
      >
        <component :is="copied ? CheckIcon : CopyIcon" class="size-3" />
        {{ copied ? 'Copied' : 'Copy' }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { CheckIcon, CopyIcon } from '@lucide/vue'
import { renderMarkdown } from '@/lib/markdown'
import ToolGroup from '@/components/ToolGroup.vue'
import ReasoningPanel from '@/components/ReasoningPanel.vue'
import { messageText, type DisplayMessage, type ToolCall } from '@/stores/session'

const props = defineProps<{ message: DisplayMessage }>()

type Block =
  | { kind: 'reasoning'; id: string; text: string; streaming: boolean }
  | { kind: 'text'; id: string; text: string; final: boolean }
  | { kind: 'tools'; id: string; tools: ToolCall[] }

// Collapse the ordered parts into render blocks: consecutive tool parts are
// grouped (parallel calls), and the trailing text/reasoning of a still-streaming
// message is marked non-final so it renders without syntax highlighting.
const blocks = computed<Block[]>(() => {
  const parts = props.message.parts
  const out: Block[] = []
  parts.forEach((part, i) => {
    const isLast = i === parts.length - 1
    if (part.kind === 'tool') {
      const prev = out[out.length - 1]
      if (prev && prev.kind === 'tools') prev.tools.push(part.tool)
      else out.push({ kind: 'tools', id: part.id, tools: [part.tool] })
    } else if (part.kind === 'reasoning') {
      out.push({ kind: 'reasoning', id: part.id, text: part.text, streaming: props.message.streaming && isLast })
    } else {
      out.push({ kind: 'text', id: part.id, text: part.text, final: !props.message.streaming || !isLast })
    }
  })
  return out
})

const text = computed(() => messageText(props.message))

const copied = ref(false)
function render(src: string, final: boolean) {
  return renderMarkdown(src, final)
}

async function copyMessage() {
  try {
    await navigator.clipboard.writeText(text.value)
    copied.value = true
    setTimeout(() => (copied.value = false), 1500)
  } catch {
    // Clipboard may be unavailable over plain http; ignore.
  }
}

// Event delegation for per-code-block copy buttons injected by the renderer.
function onCopyCode(e: MouseEvent) {
  const btn = (e.target as HTMLElement).closest('[data-copy]')
  if (!btn) return
  const pre = btn.parentElement?.querySelector('pre')
  if (pre) {
    void navigator.clipboard?.writeText(pre.textContent ?? '')
    btn.textContent = 'Copied'
    setTimeout(() => (btn.textContent = 'Copy'), 1500)
  }
}
</script>
