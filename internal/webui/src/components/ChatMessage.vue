<template>
  <div class="flex flex-col gap-1.5" :class="message.role === 'user' ? 'items-end' : 'items-start'">
    <!-- Streamed reasoning for this assistant turn, collapsed by default. -->
    <div v-if="message.role === 'assistant' && message.reasoning" class="w-full max-w-[85%]">
      <ReasoningPanel :reasoning="message.reasoning" :streaming="message.streaming" />
    </div>

    <div
      class="max-w-[85%] rounded-lg px-3.5 py-2.5 text-sm"
      :class="
        message.role === 'user'
          ? 'bg-primary text-primary-foreground'
          : 'bg-card text-card-foreground border border-border'
      "
    >
      <!-- Assistant content is markdown; user content is plain text. -->
      <div v-if="message.role === 'assistant'" class="prose-chat" v-html="rendered" />
      <div v-else class="whitespace-pre-wrap">{{ message.content }}</div>

      <span v-if="message.streaming && !message.content" class="text-muted-foreground">…</span>
    </div>

    <!-- Tool calls invoked during this assistant turn, grouped if parallel. -->
    <div v-if="message.tools.length" class="w-full max-w-[85%]">
      <ToolGroup :tools="message.tools" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { renderMarkdown } from '@/lib/markdown'
import ToolGroup from '@/components/ToolGroup.vue'
import ReasoningPanel from '@/components/ReasoningPanel.vue'
import type { DisplayMessage } from '@/stores/session'

const props = defineProps<{ message: DisplayMessage }>()

const rendered = computed(() => renderMarkdown(props.message.content ?? ''))
</script>
