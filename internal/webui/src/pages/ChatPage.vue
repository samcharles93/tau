<template>
  <ChatLayout>
    <template #header>
      <div class="flex items-center gap-2">
        <SessionSwitcher />
        <StatusBar />
        <SettingsDrawer />
      </div>
    </template>

    <div
      ref="container"
      class="h-full overflow-y-auto px-4 py-6"
      @scroll="onScroll"
    >
      <div class="mx-auto flex max-w-3xl flex-col gap-4">
        <div
          v-if="!session.messages.length"
          class="mt-24 text-center text-sm text-muted-foreground"
        >
          No messages yet.
        </div>

        <ChatMessage
          v-for="message in session.messages"
          :key="message.id"
          :message="message"
        />
      </div>
    </div>

    <ToastContainer />
    <InteractivePromptDialog />

    <template #footer>
      <div class="mx-auto max-w-3xl">
        <ChatInput :streaming="session.streaming" @submit="onSubmit" @stop="session.cancel()" />
      </div>
    </template>
  </ChatLayout>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, watch } from 'vue'
import ChatLayout from '@/layouts/ChatLayout.vue'
import ChatMessage from '@/components/ChatMessage.vue'
import ChatInput from '@/components/ChatInput.vue'
import StatusBar from '@/components/StatusBar.vue'
import SettingsDrawer from '@/components/SettingsDrawer.vue'
import SessionSwitcher from '@/components/SessionSwitcher.vue'
import ToastContainer from '@/components/ToastContainer.vue'
import InteractivePromptDialog from '@/components/InteractivePromptDialog.vue'
import { useAutoScroll } from '@/composables/useAutoScroll'
import { useSessionStore } from '@/stores/session'

const session = useSessionStore()
const { container, scrollToBottom, onScroll } = useAutoScroll()

// Re-pin to the bottom whenever the conversation grows or streams. Inspecting
// only the trailing part keeps this cheap, while still reacting to streamed
// text, reasoning, and tool output as they grow.
watch(
  () => {
    const m = session.messages[session.messages.length - 1]
    if (!m) return ''
    const last = m.parts[m.parts.length - 1]
    const tail = !last
      ? 0
      : last.kind === 'tool'
        ? `${last.tool.output.length}:${last.tool.status}`
        : last.text.length
    return `${session.messages.length}:${m.parts.length}:${tail}`
  },
  () => scrollToBottom(),
  { flush: 'post' },
)

function onSubmit(text: string) {
  session.submitPrompt(text)
  scrollToBottom()
}

// Esc anywhere on the page stops a running request — mirrors Ctrl+C in the
// TUI. We avoid intercepting Esc when a modal dialog (interactive prompt,
// settings drawer) is open because those have their own Esc semantics; the
// browser's default Esc behaviour on the input element still works, and our
// ChatInput handler runs first when the textarea is focused.
function onGlobalKeydown(e: KeyboardEvent) {
  if (e.key !== 'Escape' || !session.streaming) return
  // Don't hijack Esc when a modal dialog is open or focus is in an input
  // element (textarea, settings fields, prompt answer box) — those have
  // their own native or component-level Esc handling.
  if (session.activePrompt) return
  // Prevent hijacking Esc when any overlay sheet or modal (like Settings or
  // Sessions switcher) is open — they use Radix dialog under the hood.
  if (document.querySelector('[role="dialog"]') || document.querySelector('[role="alertdialog"]')) {
    return
  }
  const target = e.target as HTMLElement | null
  if (target && (target.isContentEditable || ['INPUT', 'TEXTAREA'].includes(target.tagName))) {
    return
  }
  e.preventDefault()
  session.cancel()
}

onMounted(() => {
  window.addEventListener('keydown', onGlobalKeydown)
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onGlobalKeydown)
})

// container is bound via the template ref; expose it so vue-tsc counts the use.
defineExpose({ container })
</script>
