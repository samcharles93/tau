<template>
  <Sheet v-model:open="open">
    <SheetTrigger as-child>
      <Button variant="ghost" size="icon-sm" title="Sessions">
        <HistoryIcon class="size-4" />
        <span class="sr-only">Sessions</span>
      </Button>
    </SheetTrigger>

    <SheetContent side="left">
      <SheetHeader>
        <SheetTitle>Sessions</SheetTitle>
        <SheetDescription>Browse, resume, export, or delete past sessions.</SheetDescription>
      </SheetHeader>

      <div class="-mx-2 flex-1 overflow-y-auto">
        <p v-if="!session.sessions.length" class="px-2 py-6 text-center text-xs text-muted-foreground">
          No saved sessions.
        </p>
        <ul v-else class="flex flex-col gap-1">
          <li
            v-for="s in session.sessions"
            :key="s.id"
            class="group rounded-md px-2 py-2 hover:bg-muted"
            :class="s.id === session.sessionId ? 'bg-muted/60' : ''"
          >
            <button class="flex w-full flex-col gap-0.5 text-left" @click="resume(s.id)">
              <span class="flex items-center gap-2 text-sm">
                <span class="font-mono text-xs">{{ s.model_id || 'session' }}</span>
                <span class="text-muted-foreground/60">·</span>
                <span class="text-xs text-muted-foreground">{{ s.message_count }} msgs</span>
              </span>
              <span class="truncate text-[11px] text-muted-foreground">{{ shortId(s.id) }} · {{ s.status }}</span>
            </button>
            <div class="mt-1 flex gap-1 opacity-0 transition-opacity group-hover:opacity-100">
              <Button variant="ghost" size="xs" @click="session.exportSession(s.id)">Export</Button>
              <Button variant="ghost" size="xs" class="text-destructive" @click="session.deleteSession(s.id)">
                Delete
              </Button>
            </div>
          </li>
        </ul>
      </div>
    </SheetContent>
  </Sheet>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { HistoryIcon } from '@lucide/vue'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'
import { Button } from '@/components/ui/button'
import { useSessionStore } from '@/stores/session'

const session = useSessionStore()
const open = ref(false)

// Refresh the list each time the panel opens.
watch(open, (isOpen) => {
  if (isOpen) session.listSessions()
})

function resume(id: string) {
  if (id !== session.sessionId) session.loadSession(id)
  open.value = false
}

function shortId(id: string): string {
  return id.length > 12 ? id.slice(0, 12) : id
}
</script>
