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
          Start the conversation. Messages sync with the terminal session.
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
import { watch } from 'vue'
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

// Re-pin to the bottom whenever the conversation grows or streams.
watch(
  () => session.messages.map((m) => m.content).join('|'),
  () => scrollToBottom(),
  { flush: 'post' },
)

function onSubmit(text: string) {
  session.submitPrompt(text)
  scrollToBottom()
}

// container is bound via the template ref; expose it so vue-tsc counts the use.
defineExpose({ container })
</script>
