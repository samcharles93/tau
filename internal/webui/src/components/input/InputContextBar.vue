<template>
  <div
    v-if="session.model || session.availableModels.length"
    ref="root"
    class="relative flex items-center gap-1.5 border-b border-border/40 px-3 py-1"
  >
    <!-- Model picker: floats upward from the context bar -->
    <div
      v-if="showPicker"
      class="absolute bottom-full left-0 right-0 mb-1 flex flex-col overflow-hidden rounded-md border border-border bg-popover shadow-lg"
      style="max-height: 320px"
    >
      <!-- Scrollable model list -->
      <div class="min-h-0 flex-1 overflow-y-auto">
        <template v-for="group in filteredGroups" :key="group.provider">
          <div class="sticky top-0 bg-popover/95 px-3 py-1 text-[10px] font-semibold uppercase tracking-wider text-muted-foreground backdrop-blur-sm">
            {{ group.provider }}
          </div>
          <button
            v-for="model in group.models"
            :key="model.id"
            :ref="(el) => setItemRef(model.id, el)"
            type="button"
            class="flex w-full items-center px-3 py-1.5 text-left text-xs"
            :class="model.id === highlighted
              ? 'bg-accent text-accent-foreground'
              : model.id === session.model
                ? 'text-primary'
                : 'hover:bg-accent/50'"
            @mouseenter="highlighted = model.id"
            @mouseleave="highlighted = null"
            @mousedown.prevent="selectModel(model)"
          >
            <span class="font-mono">{{ model.id }}</span>
          </button>
        </template>
        <div v-if="filteredGroups.length === 0" class="px-3 py-6 text-center text-xs text-muted-foreground">
          No models found.
        </div>
      </div>

      <!-- Search bar pinned to bottom -->
      <div class="border-t border-border p-2">
        <input
          ref="searchInput"
          v-model="search"
          type="text"
          placeholder="Filter models… (e.g. openai/gpt-5)"
          class="w-full rounded bg-muted px-2 py-1.5 text-xs outline-none placeholder:text-muted-foreground focus:ring-1 focus:ring-ring"
          @keydown="onSearchKeydown"
        />
      </div>
    </div>

    <!-- Compact context bar button (exactly as it was) -->
    <button
      type="button"
      class="flex items-center gap-1.5 rounded px-1.5 py-0.5 text-xs hover:bg-muted"
      @click="togglePicker"
    >
      <CpuIcon class="size-3 text-muted-foreground" />
      <span class="font-mono text-foreground/80">{{ session.model }}</span>
      <span v-if="session.provider" class="text-muted-foreground">{{ session.provider }}</span>
    </button>
    <span
      v-if="session.parameters.reasoning_effort"
      class="rounded bg-muted px-1 py-0.5 text-[10px] text-muted-foreground"
    >
      {{ session.parameters.reasoning_effort }}
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { onClickOutside } from '@vueuse/core'
import { CpuIcon } from '@lucide/vue'
import { useSessionStore } from '@/stores/session'
import type { ChatModelRef, ChatSessionPatch } from '@/lib/protocol'

const session = useSessionStore()

const root = ref<HTMLElement | null>(null)
const searchInput = ref<HTMLInputElement | null>(null)
const showPicker = ref(false)
const search = ref('')
const highlighted = ref<string | null>(null)
const itemRefs = new Map<string, Element>()

function setItemRef(id: string, el: unknown) {
  if (el instanceof Element) itemRefs.set(id, el)
  else itemRefs.delete(id)
}

onClickOutside(root, () => { showPicker.value = false })

// Group all models by provider, preserving insertion order.
const allGroups = computed(() => {
  const map = new Map<string, ChatModelRef[]>()
  for (const m of session.availableModels) {
    const key = m.provider || 'unknown'
    if (!map.has(key)) map.set(key, [])
    map.get(key)!.push(m)
  }
  return map
})

// Smart filtering:
//   "gpt-5"         → id contains "gpt-5" OR provider contains "gpt-5"
//   "openai/"       → provider starts with "openai" OR id starts with "openai/"
//   "openai/gpt-5"  → (provider starts with "openai" AND id starts with "gpt-5")
//                     OR id starts with "openai/gpt-5"  (catches openrouter-style ids)
const filteredGroups = computed(() => {
  const q = search.value.trim().toLowerCase()
  const result: { provider: string; models: ChatModelRef[] }[] = []

  for (const [provider, models] of allGroups.value) {
    let filtered: ChatModelRef[]

    if (!q) {
      filtered = models
    } else {
      const slash = q.indexOf('/')
      if (slash !== -1) {
        const providerQ = q.slice(0, slash)
        const modelQ = q.slice(slash + 1)
        filtered = models.filter((m) => {
          const nativeMatch = provider.startsWith(providerQ) && m.id.toLowerCase().startsWith(modelQ)
          const fullIdMatch = m.id.toLowerCase().startsWith(q)
          return nativeMatch || fullIdMatch
        })
      } else {
        filtered = models.filter((m) =>
          m.id.toLowerCase().includes(q) || provider.includes(q),
        )
      }
    }

    if (filtered.length) result.push({ provider, models: filtered })
  }

  return result
})

// Flat ordered list of filtered model ids for keyboard navigation.
const flatFiltered = computed(() =>
  filteredGroups.value.flatMap((g) => g.models.map((m) => m.id)),
)

function togglePicker() {
  showPicker.value = !showPicker.value
  if (showPicker.value) {
    search.value = ''
    highlighted.value = session.model || null
    nextTick(() => {
      searchInput.value?.focus()
      // Scroll current model into view.
      if (session.model) {
        nextTick(() => itemRefs.get(session.model)?.scrollIntoView({ block: 'nearest' }))
      }
    })
  }
}

function selectModel(model: ChatModelRef) {
  if (model.id === session.model) { showPicker.value = false; return }
  const patch: ChatSessionPatch = { model: { id: model.id } }
  if (model.provider) patch.provider = model.provider
  session.updateSettings(patch)
  showPicker.value = false
}

function onSearchKeydown(e: KeyboardEvent) {
  const flat = flatFiltered.value
  if (!flat.length) return

  if (e.key === 'Escape') {
    showPicker.value = false
    return
  }

  if (e.key === 'ArrowUp' || e.key === 'ArrowDown') {
    e.preventDefault()
    const idx = highlighted.value ? flat.indexOf(highlighted.value) : -1
    const next = e.key === 'ArrowDown'
      ? (idx + 1) % flat.length
      : (idx - 1 + flat.length) % flat.length
    highlighted.value = flat[next]
    nextTick(() => itemRefs.get(flat[next])?.scrollIntoView({ block: 'nearest' }))
    return
  }

  if (e.key === 'Enter') {
    e.preventDefault()
    const id = highlighted.value ?? flat[0]
    const model = session.availableModels.find((m) => m.id === id)
    if (model) selectModel(model)
  }
}
</script>
