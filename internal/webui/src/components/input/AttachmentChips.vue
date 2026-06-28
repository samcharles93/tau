<template>
  <div v-if="attachments.length" class="flex flex-wrap gap-1.5 px-3 pt-2">
    <span
      v-for="a in attachments"
      :key="a.id"
      class="flex items-center gap-1.5 rounded-md border border-border bg-muted/50 px-2 py-1 text-xs"
    >
      <ImageIcon v-if="a.mode === 'image'" class="size-3.5 shrink-0 text-muted-foreground" />
      <FileTextIcon v-else-if="a.mode === 'text'" class="size-3.5 shrink-0 text-muted-foreground" />
      <FileIcon v-else class="size-3.5 shrink-0 text-muted-foreground" />
      <span class="max-w-40 truncate font-mono">{{ a.name }}</span>
      <span class="text-muted-foreground/70">{{ formatSize(a.size) }}</span>
      <button type="button" class="text-muted-foreground hover:text-foreground" @click="emit('remove', a.id)">
        <XIcon class="size-3" />
      </button>
    </span>
  </div>
</template>

<script setup lang="ts">
import { FileIcon, FileTextIcon, ImageIcon, XIcon } from '@lucide/vue'
import type { Attachment } from '@/composables/useAttachments'

defineProps<{ attachments: Attachment[] }>()
const emit = defineEmits<{ remove: [id: string] }>()

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes}B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)}MB`
}
</script>
