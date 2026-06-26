<template>
  <DialogRoot :open="!!prompt" @update:open="(v: boolean) => { if (!v) cancel() }">
    <DialogPortal>
      <DialogOverlay
        class="fixed inset-0 z-50 bg-black/60 data-[state=open]:animate-in data-[state=open]:fade-in-0"
      />
      <DialogContent
        v-if="prompt"
        class="fixed left-1/2 top-1/2 z-50 grid w-full max-w-md -translate-x-1/2 -translate-y-1/2 gap-4 rounded-xl border border-border bg-popover p-6 text-popover-foreground shadow-lg outline-none data-[state=open]:animate-in data-[state=open]:fade-in-0 data-[state=open]:zoom-in-95"
      >
        <div class="flex flex-col gap-1.5">
          <DialogTitle class="text-sm font-semibold">{{ prompt.title || 'Tau needs input' }}</DialogTitle>
          <DialogDescription class="whitespace-pre-wrap text-xs text-muted-foreground">
            {{ prompt.message }}
          </DialogDescription>
        </div>

        <Input
          v-if="prompt.kind === 'question'"
          v-model="answer"
          placeholder="Your answer…"
          @keydown.enter="submitAnswer"
        />

        <div class="flex justify-end gap-2">
          <Button variant="ghost" @click="cancel">Cancel</Button>
          <Button v-if="prompt.kind === 'question'" :disabled="!answer.trim()" @click="submitAnswer">
            Send
          </Button>
          <Button v-else @click="confirm">Confirm</Button>
        </div>
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useSessionStore } from '@/stores/session'

const session = useSessionStore()
const prompt = computed(() => session.activePrompt)
const answer = ref('')

// Clear the draft answer whenever a new prompt appears.
watch(prompt, () => (answer.value = ''))

function confirm() {
  session.respondPrompt({ confirmed: true })
}

function cancel() {
  session.respondPrompt({ confirmed: false, canceled: true })
}

function submitAnswer() {
  if (!answer.value.trim()) return
  session.respondPrompt({ confirmed: true, response: answer.value.trim() })
}
</script>
