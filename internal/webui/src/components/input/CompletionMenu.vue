<template>
  <div
    class="absolute bottom-full left-3 right-3 mb-1 flex overflow-hidden rounded-md border border-border bg-popover shadow-lg"
  >
    <!-- Left panel: item list -->
    <div class="w-64 shrink-0 overflow-y-auto border-r border-border">
      <template v-for="(group, groupIdx) in groups" :key="group.label">
        <div
          v-if="groupIdx > 0"
          class="mx-2 my-1 h-px bg-border"
        />
        <div class="px-2 py-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
          {{ group.label }}
        </div>
        <button
          v-for="item in group.items"
          :key="item.id"
          :ref="(el) => setItemRef(item, el)"
          type="button"
          class="flex w-full items-center gap-1.5 px-2 py-1.5 text-left text-xs"
          :class="item === activeItem
            ? 'bg-accent text-accent-foreground'
            : 'hover:bg-accent/50'"
          @mousedown.prevent="$emit('accept', item)"
          @mouseenter="$emit('hover', flatIndex(item))"
        >
          <span class="min-w-0 flex-1 truncate font-mono">{{ item.name }}</span>
          <span
            v-if="item.badge"
            class="shrink-0 rounded px-1 py-0.5 text-[9px] font-medium leading-none"
            :class="item.badgeClass ?? 'bg-muted text-muted-foreground'"
          >{{ item.badge }}</span>
        </button>
      </template>
    </div>

    <!-- Right panel: description -->
    <div class="flex min-w-0 flex-1 flex-col gap-1 p-3">
      <template v-if="activeItem">
        <span class="font-mono text-sm text-foreground">{{ activeItem.name }}</span>
        <span v-if="activeItem.description" class="text-xs text-muted-foreground">{{ activeItem.description }}</span>
        <span v-if="activeItem.acceptsArgs" class="mt-auto text-[10px] text-muted-foreground/60">Accepts arguments</span>
      </template>
      <span v-else class="text-xs text-muted-foreground/50">Select an item</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue'

export interface CompletionItem {
  id: string
  group: 'commands' | 'skills' | 'extensions' | 'models' | 'sessions' | 'effort' | 'login'
  name: string
  description?: string
  badge?: string
  badgeClass?: string
  acceptsArgs?: boolean
  replaceStart?: number
  replaceEnd?: number
}

const GROUP_LABELS: Record<CompletionItem['group'], string> = {
  commands: 'Commands',
  skills: 'Skills',
  extensions: 'Extensions',
  models: 'Models',
  sessions: 'Sessions',
  effort: 'Effort',
  login: 'Providers',
}

const props = defineProps<{
  items: CompletionItem[]
  selected: number
}>()

defineEmits<{
  accept: [item: CompletionItem]
  hover: [index: number]
}>()

const itemRefs = new Map<CompletionItem, Element>()

function setItemRef(item: CompletionItem, el: unknown) {
  if (el instanceof Element) itemRefs.set(item, el)
  else itemRefs.delete(item)
}

const activeItem = computed(() => props.items[props.selected] ?? null)

const groups = computed(() => {
  const map = new Map<CompletionItem['group'], CompletionItem[]>()
  for (const item of props.items) {
    if (!map.has(item.group)) map.set(item.group, [])
    map.get(item.group)!.push(item)
  }
  return [...map.entries()].map(([key, items]) => ({ label: GROUP_LABELS[key], items }))
})

function flatIndex(item: CompletionItem): number {
  return props.items.indexOf(item)
}

watch(activeItem, (item) => {
  if (!item) return
  const el = itemRefs.get(item)
  el?.scrollIntoView({ block: 'nearest' })
})
</script>
