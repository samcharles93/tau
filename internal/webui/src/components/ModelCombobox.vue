<script setup lang="ts">
import { computed, ref } from 'vue'
import { CheckIcon, ChevronDownIcon, SearchIcon } from '@lucide/vue'
import {
  ComboboxAnchor,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxGroup,
  ComboboxInput,
  ComboboxItem,
  ComboboxItemIndicator,
  ComboboxLabel,
  ComboboxPortal,
  ComboboxRoot,
  ComboboxTrigger,
  ComboboxViewport,
} from 'reka-ui'
import type { ChatModelRef } from '@/lib/protocol'
import { cn } from '@/lib/utils'

const props = defineProps<{
  models: ChatModelRef[]
  modelValue: string
  placeholder?: string
  class?: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const search = ref('')

const selected = computed({
  get: () => props.modelValue,
  set: (val: string) => emit('update:modelValue', val),
})

// Group filtered models by provider
const groupedModels = computed(() => {
  const q = search.value.toLowerCase()
  const groups = new Map<string, ChatModelRef[]>()
  for (const m of props.models) {
    if (q && !m.id.toLowerCase().includes(q)) continue
    const provider = m.provider || 'Other'
    if (!groups.has(provider)) groups.set(provider, [])
    groups.get(provider)!.push(m)
  }
  return groups
})

// Display label for the current model
const displayLabel = computed(() => props.modelValue || props.placeholder || 'Select model')

// Bypass reka-ui built-in filtering — we filter in groupedModels computed above
function noFilter(items: string[]) {
  return items
}
</script>

<template>
  <ComboboxRoot
    v-model="selected"
    v-model:search-term="search"
    :filter-function="noFilter"
    :class="cn('relative w-full', props.class)"
    @update:model-value="search = ''"
  >
    <ComboboxAnchor
      class="border-input bg-input/30 dark:hover:bg-input/50 focus-within:border-ring focus-within:ring-ring/50 gap-1.5 rounded border px-3 py-0 text-sm transition-colors focus-within:ring-3 h-9 flex w-full items-center justify-between whitespace-nowrap"
    >
      <SearchIcon class="text-muted-foreground size-3.5 shrink-0" />
      <ComboboxInput
        :placeholder="displayLabel"
        class="flex-1 bg-transparent text-sm outline-none placeholder:text-muted-foreground min-w-0 py-2 px-1"
      />
      <ComboboxTrigger>
        <ChevronDownIcon class="text-muted-foreground size-4 shrink-0" />
      </ComboboxTrigger>
    </ComboboxAnchor>

    <ComboboxPortal>
      <ComboboxContent
        position="popper"
        class="bg-popover text-popover-foreground ring-foreground/5 z-50 mt-1 max-h-72 min-w-[var(--reka-combobox-trigger-width)] w-[var(--reka-combobox-trigger-width)] overflow-y-auto rounded-2xl shadow-2xl ring-1 animate-in fade-in-0 zoom-in-95"
      >
        <ComboboxViewport class="p-1">
          <ComboboxEmpty class="py-6 text-center text-sm text-muted-foreground">
            No models found.
          </ComboboxEmpty>

          <template v-for="[provider, items] in groupedModels" :key="provider">
            <ComboboxGroup>
              <ComboboxLabel class="px-3 py-1.5 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground">
                {{ provider }}
              </ComboboxLabel>
              <ComboboxItem
                v-for="m in items"
                :key="m.id"
                :value="m.id"
                class="focus:bg-accent focus:text-accent-foreground relative flex w-full cursor-default select-none items-center rounded-xl py-2 pl-3 pr-8 text-sm outline-hidden data-[disabled]:pointer-events-none data-[disabled]:opacity-50"
              >
                <ComboboxItemIndicator class="absolute right-2 flex size-4 items-center justify-center">
                  <CheckIcon class="size-3.5" />
                </ComboboxItemIndicator>
                <span class="font-mono text-xs">{{ m.id }}</span>
              </ComboboxItem>
            </ComboboxGroup>
          </template>
        </ComboboxViewport>
      </ComboboxContent>
    </ComboboxPortal>
  </ComboboxRoot>
</template>
