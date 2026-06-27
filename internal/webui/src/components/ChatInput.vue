<template>
  <div class="relative">
    <!-- Slash-command autocomplete, surfaced from the init command list. -->
    <div
      v-if="showMenu"
      class="absolute bottom-full left-3 mb-1 w-80 overflow-hidden rounded-md border border-border bg-popover shadow-lg"
    >
      <button
        v-for="(cmd, i) in matches"
        :key="cmd.name"
        type="button"
        class="flex w-full flex-col gap-0.5 px-3 py-1.5 text-left text-xs"
        :class="i === selected ? 'bg-muted' : ''"
        @mousedown.prevent="accept(cmd)"
        @mouseenter="selected = i"
      >
        <span class="font-mono text-foreground">{{ cmd.name }}</span>
        <span v-if="cmd.description" class="truncate text-muted-foreground">{{ cmd.description }}</span>
      </button>
    </div>

    <form class="flex items-end gap-2 p-3" @submit.prevent="submit">
      <textarea
        ref="textarea"
        v-model="draft"
        rows="1"
        :placeholder="placeholder"
        class="max-h-48 min-h-[2.5rem] flex-1 resize-none rounded-md border border-input bg-card px-3 py-2 text-sm outline-none focus:ring-1 focus:ring-ring"
        @input="autogrow"
        @keydown="onKeydown"
      />
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
        :disabled="!draft.trim()"
        class="h-10 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground disabled:opacity-40"
      >
        Send
      </button>
    </form>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useSessionStore } from '@/stores/session'
import type { CommandRef } from '@/lib/protocol'

withDefaults(defineProps<{ placeholder?: string; streaming?: boolean }>(), {
  placeholder: 'How can I assist you today?',
  streaming: false,
})

const emit = defineEmits<{ submit: [text: string]; stop: [] }>()

const session = useSessionStore()
const draft = ref('')
const textarea = ref<HTMLTextAreaElement | null>(null)
const selected = ref(0)
const menuDismissed = ref(false)

// The menu shows while typing a `/command` token (before any space/args).
const matches = computed<CommandRef[]>(() => {
  const value = draft.value
  if (!value.startsWith('/') || value.includes(' ') || value.includes('\n')) return []
  const token = value.slice(1).toLowerCase()
  return session.commands
    .filter((c) => c.name.replace(/^\//, '').toLowerCase().startsWith(token))
    .slice(0, 8)
})

const showMenu = computed(() => !menuDismissed.value && matches.value.length > 0)

function autogrow() {
  menuDismissed.value = false
  selected.value = 0
  const el = textarea.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = `${el.scrollHeight}px`
}

function onKeydown(e: KeyboardEvent) {
  if (showMenu.value) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      selected.value = (selected.value + 1) % matches.value.length
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      selected.value = (selected.value - 1 + matches.value.length) % matches.value.length
      return
    }
    if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey)) {
      e.preventDefault()
      accept(matches.value[selected.value])
      return
    }
    if (e.key === 'Escape') {
      e.preventDefault()
      menuDismissed.value = true
      return
    }
  }

  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    submit()
  }
}

function accept(cmd: CommandRef) {
  draft.value = cmd.accepts_args ? `${cmd.name} ` : cmd.name
  menuDismissed.value = true
  nextTick(() => textarea.value?.focus())
}

function submit() {
  const text = draft.value.trim()
  if (!text) return
  emit('submit', text)
  draft.value = ''
  menuDismissed.value = false
  nextTick(autogrow)
}
</script>
