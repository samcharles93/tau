<template>
  <Collapsible v-model:open="open" class="w-full rounded-md border border-border/60 bg-muted/20">
    <CollapsibleTrigger class="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-xs text-muted-foreground">
      <BrainIcon class="size-3.5 shrink-0" />
      <span>Reasoning</span>
      <span v-if="streaming" class="inline-block size-1.5 rounded-full bg-status-running animate-status-pulse" />
      <span class="flex-1" />
      <ChevronRightIcon class="size-3.5 shrink-0 transition-transform" :class="open ? 'rotate-90' : ''" />
    </CollapsibleTrigger>
    <CollapsibleContent class="overflow-hidden data-[state=closed]:animate-out data-[state=open]:animate-in">
      <pre class="max-h-72 overflow-auto whitespace-pre-wrap border-t border-border/60 px-2.5 py-2 font-mono text-[11px] leading-relaxed text-muted-foreground">{{ reasoning }}</pre>
    </CollapsibleContent>
  </Collapsible>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { BrainIcon, ChevronRightIcon } from '@lucide/vue'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'

defineProps<{ reasoning: string; streaming?: boolean }>()

// Collapsed by default; reasoning is supplementary, not the main thread.
const open = ref(false)
</script>
