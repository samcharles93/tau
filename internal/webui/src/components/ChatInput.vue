<template>
  <div
    ref="wrapper"
    class="relative rounded-md"
    :class="isOverDropZone ? 'ring-2 ring-primary' : ''"
  >
    <CompletionMenu
      v-if="showMenu"
      :items="completionItems"
      :selected="selected"
      @accept="accept"
      @hover="selected = $event"
    />

    <InputContextBar />

    <AttachmentChips :attachments="attachments.attachments.value" @remove="attachments.remove" />

    <form class="flex items-end gap-2 p-3" @submit.prevent="submit">
      <input ref="fileInput" type="file" multiple class="hidden" @change="onFilePicked" />
      <button
        type="button"
        class="flex h-10 w-10 shrink-0 items-center justify-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
        title="Attach files"
        @click="fileInput?.click()"
      >
        <PaperclipIcon class="size-4" />
      </button>
      <textarea
        ref="textarea"
        v-model="draft"
        rows="1"
        :placeholder="placeholder"
        class="max-h-48 min-h-[2.5rem] flex-1 resize-none rounded-md border border-input bg-card px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring"
        @input="onInput"
        @keydown="onKeydown"
        @paste="onPaste"
      />
      <span
        v-if="draft.length"
        class="shrink-0 self-end pb-2.5 text-xs tabular-nums text-muted-foreground/60 select-none"
      >
        {{ draft.length }}&thinsp;·&thinsp;~{{ Math.ceil(draft.length / 4) }}t
      </span>
      <button
        v-if="streaming"
        type="button"
        class="h-10 rounded-md bg-destructive/15 px-4 text-sm font-medium text-destructive hover:bg-destructive/25"
        @click="emit('stop')"
      >
        Stop
      </button>
      <button
        v-else
        type="submit"
        :disabled="!draft.trim() && !attachments.hasAttachments.value"
        class="h-10 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground disabled:opacity-40"
      >
        Send
      </button>
    </form>

    <!-- Token usage / cost for the current session, pinned bottom-right. -->
    <div class="flex justify-end px-3 pb-1.5">
      <UsageIndicator />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { useDropZone } from '@vueuse/core'
import { PaperclipIcon } from '@lucide/vue'
import UsageIndicator from '@/components/UsageIndicator.vue'
import CompletionMenu, { type CompletionItem } from '@/components/input/CompletionMenu.vue'
import InputContextBar from '@/components/input/InputContextBar.vue'
import AttachmentChips from '@/components/input/AttachmentChips.vue'
import { usePromptHistory } from '@/composables/usePromptHistory'
import { useAttachments } from '@/composables/useAttachments'
import { useSessionStore } from '@/stores/session'
import type { ChatSessionPatch } from '@/lib/protocol'

withDefaults(defineProps<{ placeholder?: string; streaming?: boolean }>(), {
  placeholder: 'How can I assist you today?',
  streaming: false,
})

const emit = defineEmits<{ submit: [text: string]; stop: [] }>()

const session = useSessionStore()
const history = usePromptHistory()
const attachments = useAttachments()

const draft = ref('')
const textarea = ref<HTMLTextAreaElement | null>(null)
const fileInput = ref<HTMLInputElement | null>(null)
const wrapper = ref<HTMLElement | null>(null)
const selected = ref(0)
const menuDismissed = ref(false)

const slashMode = computed(() => draft.value.startsWith('/') && !/[ \n]/.test(draft.value))

const completionItems = computed<CompletionItem[]>(() => {
  if (!slashMode.value) return []
  const token = draft.value.slice(1).toLowerCase()
  return session.commands
    .filter((c) => c.name.replace(/^\//, '').toLowerCase().startsWith(token))
    .slice(0, 24)
    .map((c) => ({
      id: `cmd:${c.name}`,
      group: 'commands' as const,
      name: c.name,
      description: c.description,
      acceptsArgs: c.accepts_args,
    }))
})

const showMenu = computed(() => !menuDismissed.value && completionItems.value.length > 0)

const { isOverDropZone } = useDropZone(wrapper, { onDrop })

onMounted(() => {
  draft.value = history.loadDraft()
})

function autogrow() {
  const el = textarea.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = `${el.scrollHeight}px`
}

function onInput() {
  menuDismissed.value = false
  selected.value = 0
  history.saveDraft(draft.value)
  history.resetCursor()
  autogrow()
}

function isOnFirstLine(el: HTMLTextAreaElement): boolean {
  return !el.value.slice(0, el.selectionStart ?? 0).includes('\n')
}

function isOnLastLine(el: HTMLTextAreaElement): boolean {
  return !el.value.slice(el.selectionEnd ?? el.value.length).includes('\n')
}

function onKeydown(e: KeyboardEvent) {
  if (showMenu.value) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      selected.value = (selected.value + 1) % completionItems.value.length
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      selected.value = (selected.value - 1 + completionItems.value.length) % completionItems.value.length
      return
    }
    if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey)) {
      e.preventDefault()
      accept(completionItems.value[selected.value])
      return
    }
    if (e.key === 'Escape') {
      e.preventDefault()
      menuDismissed.value = true
      return
    }
  }

  const el = textarea.value
  if (e.key === 'ArrowUp' && el && isOnFirstLine(el)) {
    const entry = history.prev()
    if (entry != null) {
      e.preventDefault()
      draft.value = entry
      nextTick(autogrow)
    }
    return
  }
  if (e.key === 'ArrowDown' && el && isOnLastLine(el)) {
    const entry = history.next()
    if (entry != null) {
      e.preventDefault()
      draft.value = entry
      nextTick(autogrow)
    }
    return
  }

  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    submit()
  }
}

function accept(item: CompletionItem) {
  if (!item) return
  draft.value = item.acceptsArgs ? `${item.name} ` : item.name
  menuDismissed.value = true
  nextTick(() => textarea.value?.focus())
}

async function onFilePicked(e: Event) {
  const files = (e.target as HTMLInputElement).files
  if (files) await Promise.all([...files].map((f) => attachments.addFile(f)))
  if (fileInput.value) fileInput.value.value = ''
}

async function onDrop(files: File[] | null) {
  if (files) await Promise.all(files.map((f) => attachments.addFile(f)))
}

async function onPaste(e: ClipboardEvent) {
  const files = [...(e.clipboardData?.files ?? [])]
  if (!files.length) return
  e.preventDefault()
  await Promise.all(files.map((f) => attachments.addFile(f)))
}

/** Parse and dispatch slash commands. Returns true if handled, false if should be sent as prompt. */
function handleSlashCommand(text: string): boolean {
  const parts = text.split(/\s+/)
  const name = parts[0].toLowerCase().replace(/^\//, '')
  const args = text.slice(parts[0].length).trim()

  switch (name) {
    case 'model': {
      const modelId = args.trim()
      if (!modelId) {
        return false
      }
      const model = session.availableModels.find((m) => m.id === modelId)
      if (!model) {
        session.apply({ type: 'ChatNotificationEvent', payload: { message: `model "${modelId}" not found`, level: 'error' } })
        return true
      }
      const patch: ChatSessionPatch = { model: { id: model.id } }
      if (model.provider) {
        patch.provider = model.provider
      }
      session.updateSettings(patch)
      session.apply({ type: 'ChatNotificationEvent', payload: { message: `model: ${model.id}${model.provider ? ` (${model.provider})` : ''}`, level: 'info' } })
      return true
    }

    case 'system': {
      const prompt = args.trim()
      if (!prompt) {
        session.apply({ type: 'ChatNotificationEvent', payload: { message: 'usage: /system <prompt>', level: 'error' } })
        return true
      }
      const patch: ChatSessionPatch = { system_prompt: prompt }
      session.updateSettings(patch)
      session.apply({ type: 'ChatNotificationEvent', payload: { message: 'system prompt updated', level: 'info' } })
      return true
    }

    case 'effort': {
      const level = args.trim().toLowerCase() || 'off'
      const patch: ChatSessionPatch = { reasoning_effort: level }
      session.updateSettings(patch)
      session.apply({ type: 'ChatNotificationEvent', payload: { message: `effort: ${level}`, level: 'info' } })
      return true
    }

    case 'new':
    case 'clear':
    case 'reset': {
      session.resetSession()
      session.apply({ type: 'ChatNotificationEvent', payload: { message: 'conversation cleared', level: 'info' } })
      return true
    }

    case 'resume': {
      const id = args.trim()
      if (!id) return false
      session.loadSession(id)
      return true
    }

    case 'session': {
      const subParts = args.split(/\s+/)
      const subCmd = subParts[0]?.toLowerCase()
      const subArgs = subParts.slice(1).join(' ')

      if (!subCmd || subCmd === 'list') {
        session.listSessions(10)
        return true
      }

      if (subCmd === 'info') {
        // Show info from the sessions list
        const id = subArgs.trim()
        const found = session.sessions.find((s) => s.id === id)
        if (found) {
          session.apply({ type: 'ChatNotificationEvent', payload: { message: `${found.id} — ${found.message_count} messages, ${found.total_tokens} tokens`, level: 'info' } })
        } else {
          session.apply({ type: 'ChatNotificationEvent', payload: { message: 'session not found', level: 'error' } })
        }
        return true
      }

      if (subCmd === 'export') {
        const id = subArgs.split(/\s+/)[0]
        if (!id) {
          session.apply({ type: 'ChatNotificationEvent', payload: { message: 'usage: /session export <id>', level: 'error' } })
          return true
        }
        session.exportSession(id, 'jsonl')
        return true
      }

      if (subCmd === 'delete') {
        const id = subArgs.split(/\s+/)[0]
        if (!id) {
          session.apply({ type: 'ChatNotificationEvent', payload: { message: 'usage: /session delete <id>', level: 'error' } })
          return true
        }
        session.deleteSession(id)
        return true
      }

      // No subcommand: treat as session ID to load
      if (subCmd) {
        session.loadSession(subCmd)
        return true
      }
      return false
    }

    case 'reload': {
      session.reloadExtensions()
      session.apply({ type: 'ChatNotificationEvent', payload: { message: 'reloading extensions...', level: 'info' } })
      return true
    }

    case 'refresh': {
      // Model refresh is handled by the backend when we request models
      session.apply({ type: 'ChatNotificationEvent', payload: { message: 'model refresh not available in web UI', level: 'warn' } })
      return true
    }

    case 'help':
    case '?': {
      const lines = ['Commands:']
      for (const c of session.commands) {
        const label = c.label || '/' + c.name
        lines.push(`  ${label} ${c.accepts_args ? '<args>' : ''} — ${c.description || ''}`)
      }
      session.apply({ type: 'ChatNotificationEvent', payload: { message: lines.join('\n'), level: 'info' } })
      return true
    }

    default:
      // Unknown command: let it through as a prompt (backend will handle or error)
      return false
  }
}

function submit() {
  const text = draft.value.trim()
  const suffix = attachments.buildPromptSuffix()
  const full = `${text}${suffix}`.trim()
  if (!full) return
  
  // Check for slash commands first
  if (text.startsWith('/')) {
    if (handleSlashCommand(text)) {
      // Command was handled, clear input and return
      draft.value = ''
      attachments.clear()
      menuDismissed.value = false
      nextTick(autogrow)
      return
    }
    // If not handled, fall through to send as prompt
  }
  
  history.push(text || full)
  history.saveDraft('')
  emit('submit', full)
  draft.value = ''
  attachments.clear()
  menuDismissed.value = false
  nextTick(autogrow)
}
</script>
