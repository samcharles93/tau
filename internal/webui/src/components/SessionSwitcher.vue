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
            v-for="row in sessionTree"
            :key="row.session.id"
            class="group rounded-md px-2 py-2 hover:bg-muted"
            :class="row.session.id === session.sessionId ? 'bg-muted/60' : ''"
            :style="{ paddingLeft: `${8 + row.depth * 16}px` }"
          >
            <button class="flex w-full flex-col gap-0.5 text-left" @click="resume(row.session.id)">
              <span class="flex items-center gap-2 text-sm">
                <span v-if="row.depth > 0" class="text-muted-foreground/60">└─</span>
                <span class="font-mono text-xs">{{ row.session.model_id || 'session' }}</span>
                <span class="text-muted-foreground/60">·</span>
                <span class="text-xs text-muted-foreground">{{ row.session.message_count }} msgs</span>
              </span>
              <span class="truncate text-[11px] text-muted-foreground">
                {{ shortId(row.session.id) }} · {{ row.session.status }}
                <template v-if="row.session.agent_instance_id"> · {{ agentLabel(row.session) }}</template>
              </span>
            </button>
            <div class="mt-1 flex gap-1 opacity-0 transition-opacity group-hover:opacity-100">
              <Button variant="ghost" size="xs" @click="session.exportSession(row.session.id)">Export</Button>
              <Button variant="ghost" size="xs" class="text-destructive" @click="session.deleteSession(row.session.id)">
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
import { computed, ref, watch } from 'vue'
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
import type { SessionSummary } from '@/lib/protocol'

const session = useSessionStore()
const open = ref(false)

// Refresh the list each time the panel opens.
watch(open, (isOpen) => {
  if (isOpen) session.listSessions()
})

interface SessionRow {
  session: SessionSummary
  depth: number
}

// Builds a depth-first lineage tree from the flat session list using
// parent_session_id, mirroring internal/tui2's sessionSummariesText.
// Sessions whose parent isn't present in this page render as roots.
const sessionTree = computed<SessionRow[]>(() => {
  const byId = new Set(session.sessions.map((s) => s.id))
  const childrenOf = new Map<string, SessionSummary[]>()
  const roots: SessionSummary[] = []

  for (const s of session.sessions) {
    if (s.parent_session_id && byId.has(s.parent_session_id)) {
      const siblings = childrenOf.get(s.parent_session_id) ?? []
      siblings.push(s)
      childrenOf.set(s.parent_session_id, siblings)
    } else {
      roots.push(s)
    }
  }

  const rows: SessionRow[] = []
  function visit(s: SessionSummary, depth: number) {
    rows.push({ session: s, depth })
    for (const child of childrenOf.get(s.id) ?? []) {
      visit(child, depth + 1)
    }
  }
  for (const root of roots) visit(root, 0)
  return rows
})

function resume(id: string) {
  if (id !== session.sessionId) session.loadSession(id)
  open.value = false
}

function shortId(id: string): string {
  return id.length > 12 ? id.slice(0, 12) : id
}

function agentLabel(s: SessionSummary): string {
  if (!s.agent_instance_id) return ''
  let name = s.agent_spec_name ?? ''
  if (!name) {
    const idx = s.agent_instance_id.indexOf('#')
    if (idx >= 0) name = s.agent_instance_id.slice(0, idx)
    else name = s.agent_instance_id
  }
  const suffix = s.agent_instance_id.includes('#')
    ? s.agent_instance_id.split('#')[1]
    : s.agent_instance_id
  return `agent ${name} · ${suffix}`
}
</script>
