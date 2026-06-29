<template>
  <div class="pointer-events-none fixed top-4 right-4 z-50 flex w-80 flex-col gap-2">
    <TransitionGroup name="toast">
      <div
        v-for="notice in session.notices"
        :key="notice.id"
        class="pointer-events-auto flex items-start gap-2 rounded-md border px-3 py-2 text-xs shadow-lg backdrop-blur"
        :class="toneClass(notice.level)"
      >
        <component :is="icon(notice.level)" class="mt-0.5 size-3.5 shrink-0" />
        <span class="flex-1 break-words max-h-32 overflow-y-auto">{{ notice.message }}</span>
        <button class="shrink-0 opacity-60 hover:opacity-100" @click="session.dismissNotice(notice.id)">
          <XIcon class="size-3.5" />
        </button>
      </div>
    </TransitionGroup>
  </div>
</template>

<script setup lang="ts">
import { watch } from 'vue'
import { CircleAlertIcon, InfoIcon, TriangleAlertIcon, XIcon } from '@lucide/vue'
import { useSessionStore } from '@/stores/session'
import type { Notice } from '@/stores/session'

const session = useSessionStore()

const timers = new Map<string, ReturnType<typeof setTimeout>>()

// Auto-dismiss info and warnings; errors stay until the user dismisses them.
watch(
  () => session.notices.map((n) => n.id),
  () => {
    for (const notice of session.notices) {
      if (notice.level === 'error' || timers.has(notice.id)) continue
      timers.set(
        notice.id,
        setTimeout(() => {
          session.dismissNotice(notice.id)
          timers.delete(notice.id)
        }, 5000),
      )
    }
  },
)

function toneClass(level: Notice['level']): string {
  switch (level) {
    case 'error':
      return 'border-destructive/40 bg-destructive/10 text-destructive'
    case 'warn':
      return 'border-chart-3/40 bg-chart-3/10 text-foreground'
    default:
      return 'border-border bg-popover text-foreground'
  }
}

function icon(level: Notice['level']) {
  switch (level) {
    case 'error':
      return CircleAlertIcon
    case 'warn':
      return TriangleAlertIcon
    default:
      return InfoIcon
  }
}
</script>

<style scoped>
.toast-enter-active,
.toast-leave-active {
  transition: all 0.2s ease;
}
.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateX(1rem);
}
</style>
