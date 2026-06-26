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
          Changes apply to the shared session, so the terminal sees them too.
        </SheetDescription>
      </SheetHeader>

      <div class="flex flex-col gap-4 py-2">
        <div class="flex flex-col gap-1.5">
          <Label for="set-model">Model</Label>
          <Input
            id="set-model"
            v-model="form.model"
            placeholder="model id"
            @keydown.enter="applyModel"
            @blur="applyModel"
          />
        </div>

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
      </div>
    </SheetContent>
  </Sheet>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
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

const form = reactive({
  model: '',
  temperature: 0,
  maxTokens: 0,
  reasoning: NONE,
})

// Refresh the form from authoritative session state each time the drawer opens.
watch(open, (isOpen) => {
  if (!isOpen) return
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

function applyModel() {
  const id = form.model.trim()
  if (id && id !== session.model) apply({ model: { id } })
}

function applyReasoning(value: unknown) {
  const v = typeof value === 'string' ? value : NONE
  apply({ reasoning_effort: v === NONE ? '' : v })
}
</script>
