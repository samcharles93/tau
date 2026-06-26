<template>
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
</template>

<script setup lang="ts">
import { nextTick, ref } from 'vue'

withDefaults(defineProps<{ placeholder?: string; streaming?: boolean }>(), {
  placeholder: 'Message tau…  (Enter to send, Shift+Enter for newline)',
  streaming: false,
})

const emit = defineEmits<{ submit: [text: string]; stop: [] }>()

const draft = ref('')
const textarea = ref<HTMLTextAreaElement | null>(null)

function autogrow() {
  const el = textarea.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = `${el.scrollHeight}px`
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    submit()
  }
}

function submit() {
  const text = draft.value.trim()
  if (!text) return
  emit('submit', text)
  draft.value = ''
  nextTick(autogrow)
}
</script>
