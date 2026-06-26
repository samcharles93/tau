<template>
  <!-- A single tool needs no group chrome. -->
  <div v-if="tools.length === 1" class="w-full">
    <ToolCard :tool="tools[0]" />
  </div>

  <!-- Multiple parallel tool calls in one turn are grouped and collapsible. -->
  <Collapsible v-else v-model:open="open" class="w-full rounded-md border border-border bg-muted/20">
    <CollapsibleTrigger class="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-xs">
      <ChevronRightIcon
        class="size-3.5 shrink-0 text-muted-foreground transition-transform"
        :class="open ? 'rotate-90' : ''"
      />
      <span class="font-medium text-foreground">{{ tools.length }} tool calls</span>
      <span class="flex-1" />
      <span v-if="runningCount" class="flex items-center gap-1 text-muted-foreground">
        <span class="inline-block size-1.5 rounded-full bg-status-running animate-status-pulse" />
        {{ runningCount }} running
      </span>
      <span v-if="errorCount" class="flex items-center gap-1 text-status-error">
        <span class="inline-block size-1.5 rounded-full bg-status-error" />
        {{ errorCount }}
      </span>
    </CollapsibleTrigger>

    <CollapsibleContent class="overflow-hidden data-[state=closed]:animate-out data-[state=open]:animate-in">
      <div class="flex flex-col gap-1 border-t border-border p-1.5">
        <ToolCard v-for="tool in tools" :key="tool.callId" :tool="tool" />
      </div>
    </CollapsibleContent>
  </Collapsible>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ChevronRightIcon } from '@lucide/vue'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import ToolCard from '@/components/ToolCard.vue'
import type { ToolCall } from '@/stores/session'

const props = defineProps<{ tools: ToolCall[] }>()

// Expanded by default while anything is still running, so progress is visible.
const open = ref(true)

const runningCount = computed(() => props.tools.filter((t) => t.status === 'running').length)
const errorCount = computed(() => props.tools.filter((t) => t.status === 'error').length)
</script>
