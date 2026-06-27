<template>
  <div class="flex items-center gap-3 text-xs text-muted-foreground">
    <span class="flex items-center gap-1.5">
      <span
        class="inline-block size-2 rounded-full"
        :class="dotClass"
        :title="status"
      />
      <span class="capitalize">{{ status }}</span>
    </span>

    <span v-if="session.model" class="font-mono">{{ session.model }}</span>
    <span v-if="session.provider" class="text-muted-foreground/70">{{ session.provider }}</span>

    <span v-if="session.skills.length > 0" class="flex items-center gap-1">
      <ZapIcon class="size-3" />
      {{ session.skills.length }} skill{{ session.skills.length !== 1 ? 's' : '' }}
    </span>

    <span v-if="clients !== null" class="flex items-center gap-1" :title="`${clients} web client(s) connected`">
      <UsersIcon class="size-3" />
      {{ clients }}
    </span>

    <button
      type="button"
      class="flex items-center gap-1 rounded px-1.5 py-0.5 font-mono hover:bg-muted hover:text-foreground"
      :title="copied ? 'Copied!' : 'Copy URL'"
      @click="copyUrl"
    >
      <component :is="copied ? CheckIcon : CopyIcon" class="size-3" />
      {{ host }}
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { CheckIcon, CopyIcon, UsersIcon, ZapIcon } from '@lucide/vue'
import { useConnection } from '@/composables/useConnection'
import { useSessionStore } from '@/stores/session'

const status = useConnection()
const session = useSessionStore()

const host = computed(() => location.host)
const clients = ref<number | null>(null)
const copied = ref(false)

const dotClass = computed(() => {
  switch (status.value) {
    case 'open':
      return 'bg-status-ok animate-status-pulse'
    case 'connecting':
      return 'bg-status-running animate-status-pulse'
    default:
      return 'bg-status-error'
  }
})

async function copyUrl() {
  try {
    await navigator.clipboard.writeText(location.origin)
    copied.value = true
    setTimeout(() => (copied.value = false), 1500)
  } catch {
    // Clipboard may be unavailable over plain http; ignore.
  }
}

let timer: ReturnType<typeof setInterval> | null = null

async function pollHealth() {
  try {
    const resp = await fetch('/health', { cache: 'no-store' })
    if (!resp.ok) return
    const body = (await resp.json()) as { clients?: number }
    if (typeof body.clients === 'number') clients.value = body.clients
  } catch {
    // Server may be momentarily unavailable during reconnect; keep last value.
  }
}

onMounted(() => {
  pollHealth()
  timer = setInterval(pollHealth, 30000)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
})
</script>
