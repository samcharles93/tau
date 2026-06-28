<template>
  <Sheet v-model:open="open">
    <SheetTrigger as-child>
      <Button variant="ghost" size="icon-sm" title="Session settings">
        <Settings2Icon class="size-4" />
        <span class="sr-only">Session settings</span>
      </Button>
    </SheetTrigger>

    <SheetContent side="right">
      <SheetHeader>
        <SheetTitle>Session settings</SheetTitle>
        <SheetDescription>
          Changes affect the current session only.
        </SheetDescription>
      </SheetHeader>

      <div class="flex flex-col gap-4 py-2 overflow-y-auto flex-1 min-h-0">
        <!-- Provider -->
        <div v-if="hasMultipleProviders" class="flex flex-col gap-1.5">
          <Label for="set-provider">Provider</Label>
          <Select v-model="form.provider" @update:model-value="applyProvider">
            <SelectTrigger id="set-provider">
              <SelectValue placeholder="Select provider" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="p in session.providers" :key="p" :value="p">{{ p }}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div v-else-if="session.provider" class="flex flex-col gap-1.5">
          <Label>Provider</Label>
          <div class="rounded-md border border-border bg-muted/50 px-3 py-2 text-sm text-muted-foreground">
            {{ session.provider }}
          </div>
        </div>

        <!-- Temperature -->
        <div class="flex flex-col gap-1.5">
          <Label for="set-temp">Temperature</Label>
          <Input
            id="set-temp"
            v-model.number="form.temperature"
            type="number"
            step="0.1"
            min="0"
            max="2"
            @change="apply({ temperature: clampNumber(form.temperature, 0, 2) })"
          />
        </div>

        <!-- Max tokens -->
        <div class="flex flex-col gap-1.5">
          <Label for="set-maxtokens">Max tokens</Label>
          <Input
            id="set-maxtokens"
            v-model.number="form.maxTokens"
            type="number"
            step="256"
            min="0"
            @change="apply({ max_tokens: Math.max(0, Math.trunc(form.maxTokens || 0)) })"
          />
        </div>

        <!-- Reasoning effort -->
        <div class="flex flex-col gap-1.5">
          <Label>Reasoning effort</Label>
          <Select v-model="form.reasoning" @update:model-value="applyReasoning">
            <SelectTrigger>
              <SelectValue placeholder="Select effort" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="opt in reasoningOptions" :key="opt.value" :value="opt.value">
                {{ opt.label }}
              </SelectItem>
            </SelectContent>
          </Select>
        </div>

        <!-- Skills -->
        <div class="border-t border-border pt-3 mt-1">
          <Label class="mb-2 block">Skills</Label>
          <div v-if="session.skills.length === 0" class="text-xs text-muted-foreground italic">
            No skills discovered
          </div>
          <div v-else class="flex flex-col gap-2">
            <div v-for="s in session.skills" :key="s.name" class="flex flex-col gap-0.5">
              <div class="flex items-center gap-1.5">
                <span class="font-mono text-xs">{{ s.name }}</span>
                <span
                  class="rounded px-1 py-0.5 text-[10px] font-medium leading-none"
                  :class="s.scope === 'user'
                    ? 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400'
                    : 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400'"
                >
                  {{ s.scope }}
                </span>
              </div>
              <span v-if="s.description" class="text-xs text-muted-foreground">{{ s.description }}</span>
            </div>
          </div>
        </div>
      </div>
    </SheetContent>
  </Sheet>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { Settings2Icon } from '@lucide/vue'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useSessionStore } from '@/stores/session'
import type { ChatSessionPatch } from '@/lib/protocol'

const NONE = 'none'

const session = useSessionStore()
const open = ref(false)
const hasMultipleProviders = computed(() => (session.providers?.length ?? 0) > 1)

// reasoningOptions builds the selectable effort levels from the current
// model's advertised reasoning_efforts. Only "None" is offered when the
// model doesn't advertise its levels — tau doesn't guess wire values.
const reasoningOptions = computed(() => {
  const modelRef = session.availableModels.find((m) => m.id === session.model)
  const efforts = modelRef?.reasoning_efforts ?? []
  return [
    { value: NONE, label: 'None' },
    ...efforts.map((e) => ({ value: e, label: e.charAt(0).toUpperCase() + e.slice(1) })),
  ]
})

const form = reactive({
  provider: '',
  temperature: 0,
  maxTokens: 0,
  reasoning: NONE,
})

// Refresh the form from authoritative session state each time the drawer opens.
watch(open, (isOpen) => {
  if (!isOpen) return
  form.provider = session.provider
  form.temperature = session.parameters.temperature
  form.maxTokens = session.parameters.max_tokens
  form.reasoning = session.parameters.reasoning_effort || NONE
})

function clampNumber(n: number, lo: number, hi: number): number {
  if (Number.isNaN(n)) return lo
  return Math.min(hi, Math.max(lo, n))
}

function apply(patch: ChatSessionPatch) {
  session.updateSettings(patch)
}

function applyProvider(value: unknown) {
  const provider = typeof value === 'string' ? value.trim() : ''
  if (provider && provider !== session.provider) {
    apply({ provider, model: { id: session.model } })
  }
}

function applyReasoning(value: unknown) {
  const v = typeof value === 'string' ? value : NONE
  apply({ reasoning_effort: v === NONE ? '' : v })
}

</script>
