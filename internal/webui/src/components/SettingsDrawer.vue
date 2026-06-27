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

      <div class="flex flex-col gap-4 py-2">
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

        <!-- Model: dropdown from available refs, or free-text fallback -->
        <div class="flex flex-col gap-1.5">
          <Label for="set-model">Model</Label>
          <Select v-if="session.availableModels.length" v-model="form.model" @update:model-value="applyModelValue">
            <SelectTrigger id="set-model">
              <SelectValue placeholder="Select model" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem v-for="m in session.availableModels" :key="m.id" :value="m.id">{{ m.id }}</SelectItem>
            </SelectContent>
          </Select>
          <Input
            v-else
            id="set-model"
            v-model="form.model"
            placeholder="model id"
            @keydown.enter="applyModel"
            @blur="applyModel"
          />
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
const reasoningOptions = [
  { value: NONE, label: 'None' },
  { value: 'minimal', label: 'Minimal' },
  { value: 'low', label: 'Low' },
  { value: 'medium', label: 'Medium' },
  { value: 'high', label: 'High' },
]

const session = useSessionStore()
const open = ref(false)
const hasMultipleProviders = computed(() => (session.providers?.length ?? 0) > 1)

const form = reactive({
  provider: '',
  model: '',
  temperature: 0,
  maxTokens: 0,
  reasoning: NONE,
})

// Refresh the form from authoritative session state each time the drawer opens.
watch(open, (isOpen) => {
  if (!isOpen) return
  form.provider = session.provider
  form.model = session.model
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
    // When switching provider, send the provider name along with current model.
    apply({ provider, model: { id: form.model || session.model } })
  }
}

// applyModelById switches the session to a model, also switching the provider
// when the model is tagged with one (aggregated cross-provider list) so the
// backend routes to the right provider instead of keeping the previous one.
function applyModelById(id: string) {
  if (!id || id === session.model) return
  const ref = session.availableModels.find((m) => m.id === id)
  const patch: ChatSessionPatch = { model: { id } }
  if (ref?.provider) patch.provider = ref.provider
  apply(patch)
}

function applyModel() {
  applyModelById(form.model.trim())
}

function applyModelValue(value: unknown) {
  applyModelById(typeof value === 'string' ? value.trim() : '')
}

function applyReasoning(value: unknown) {
  const v = typeof value === 'string' ? value : NONE
  apply({ reasoning_effort: v === NONE ? '' : v })
}
</script>
