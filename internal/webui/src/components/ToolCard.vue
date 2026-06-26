<template>
  <Collapsible
    v-model:open="open"
    class="rounded-md border border-border bg-muted/30"
  >
    <CollapsibleTrigger
      class="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-xs"
    >
      <span class="inline-block size-1.5 shrink-0 rounded-full" :class="dotClass" />
      <span class="font-mono text-foreground">{{ tool.name }}</span>
      <span class="min-w-0 flex-1 truncate text-muted-foreground">{{ tool.argumentsSummary }}</span>
      <Badge :variant="badgeVariant" class="shrink-0">{{ statusLabel }}</Badge>
      <ChevronRightIcon class="size-3.5 shrink-0 text-muted-foreground transition-transform" :class="open ? 'rotate-90' : ''" />
    </CollapsibleTrigger>

    <CollapsibleContent class="overflow-hidden data-[state=closed]:animate-out data-[state=open]:animate-in">
      <div class="space-y-2 border-t border-border px-2.5 py-2 text-xs">
        <div>
          <div class="mb-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">Arguments</div>
          <pre class="overflow-x-auto rounded bg-background/60 p-2 font-mono text-[11px] text-foreground">{{ tool.argumentsSummary || '(none)' }}</pre>
        </div>
        <div v-if="tool.output">
          <div class="mb-0.5 flex items-center gap-1.5 text-[10px] uppercase tracking-wide text-muted-foreground">
            Output
            <span v-if="tool.status === 'running'" class="inline-block size-1 rounded-full bg-status-running animate-status-pulse" />
          </div>
          <pre class="max-h-64 overflow-auto whitespace-pre-wrap rounded bg-background/60 p-2 font-mono text-[11px] text-foreground">{{ tool.output }}</pre>
        </div>
        <div v-if="tool.resultSummary !== undefined">
          <div class="mb-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">Result</div>
          <pre
            class="overflow-x-auto rounded bg-background/60 p-2 font-mono text-[11px]"
            :class="tool.status === 'error' ? 'text-status-error' : 'text-foreground'"
          >{{ tool.resultSummary || '(none)' }}</pre>
        </div>
      </div>
    </CollapsibleContent>
  </Collapsible>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { ChevronRightIcon } from '@lucide/vue'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Badge } from '@/components/ui/badge'
import type { ToolCall } from '@/stores/session'

const props = defineProps<{ tool: ToolCall }>()

const open = ref(false)

const dotClass = computed(() => {
  switch (props.tool.status) {
    case 'ok':
      return 'bg-status-ok'
    case 'error':
      return 'bg-status-error'
    default:
      return 'bg-status-running animate-status-pulse'
  }
})

const statusLabel = computed(() => {
  switch (props.tool.status) {
    case 'ok':
      return 'done'
    case 'error':
      return 'error'
    default:
      return 'running'
  }
})

const badgeVariant = computed<'default' | 'secondary' | 'destructive'>(() => {
  switch (props.tool.status) {
    case 'ok':
      return 'secondary'
    case 'error':
      return 'destructive'
    default:
      return 'default'
  }
})
</script>
