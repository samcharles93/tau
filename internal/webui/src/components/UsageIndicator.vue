<template>
  <div
    v-if="totalTokens > 0"
    class="flex items-center gap-1.5 font-mono text-[11px] text-muted-foreground"
    :title="title"
  >
    <span v-if="costText">{{ costText }}</span>
    <span v-if="costText" class="text-muted-foreground/40">|</span>
    <span :class="nearLimit ? 'text-status-error' : ''">
      {{ formatNum(totalTokens) }}<template v-if="contextWindow > 0"> / {{ formatNum(contextWindow) }}</template>
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useSessionStore } from '@/stores/session'

const session = useSessionStore()

const totalTokens = computed(() => session.usage.total_tokens ?? 0)
const contextWindow = computed(() => session.contextWindow)

const nearLimit = computed(
  () => contextWindow.value > 0 && totalTokens.value / contextWindow.value >= 0.9,
)

// Cost in USD: prices are per 1M tokens, split across prompt and completion.
const costValue = computed(() => {
  const u = session.usage
  const c = session.cost
  const input = ((u.prompt_tokens ?? 0) * (c.input ?? 0)) / 1_000_000
  const output = ((u.completion_tokens ?? u.output_tokens ?? 0) * (c.output ?? 0)) / 1_000_000
  return input + output
})

const costText = computed(() => (costValue.value > 0 ? `$ ${costValue.value.toFixed(2)}` : ''))

const title = computed(() => {
  const u = session.usage
  const parts = [`prompt: ${formatNum(u.prompt_tokens ?? 0)}`, `completion: ${formatNum(u.completion_tokens ?? u.output_tokens ?? 0)}`]
  if (contextWindow.value > 0) parts.push(`context window: ${formatNum(contextWindow.value)}`)
  return parts.join(' · ')
})

function formatNum(n: number): string {
  return n.toLocaleString('en-US')
}
</script>
