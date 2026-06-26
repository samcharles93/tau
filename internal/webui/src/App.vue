<template>
  <RouterView />
</template>

<script setup lang="ts">
import { RouterView } from 'vue-router'
import { useWebSocket } from '@/composables/useWebSocket'
import { useSessionStore } from '@/stores/session'
import { provideConnection } from '@/composables/useConnection'

const session = useSessionStore()

const { status, send } = useWebSocket('/ws', {
  onMessage: (msg) => session.apply(msg),
})

session.bindSender(send)
provideConnection(status)
</script>
