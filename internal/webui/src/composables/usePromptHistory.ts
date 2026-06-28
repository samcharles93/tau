import { ref } from 'vue'
import { useLocalStorage, watchDebounced } from '@vueuse/core'

const HISTORY_KEY = 'tau:prompt-history'
const DRAFT_KEY = 'tau:draft'
const MAX_ENTRIES = 100

export function usePromptHistory() {
  const history = useLocalStorage<string[]>(HISTORY_KEY, [])
  const draftStorage = useLocalStorage<string>(DRAFT_KEY, '')

  const cursor = ref(-1)
  const pendingDraft = ref('')

  function push(text: string) {
    if (!text.trim()) return
    const idx = history.value.indexOf(text)
    if (idx !== -1) history.value.splice(idx, 1)
    history.value.unshift(text)
    if (history.value.length > MAX_ENTRIES) history.value.length = MAX_ENTRIES
    cursor.value = -1
  }

  function prev(): string | null {
    if (history.value.length === 0) return null
    const next = cursor.value + 1
    if (next >= history.value.length) return null
    cursor.value = next
    return history.value[cursor.value]
  }

  function next(): string | null {
    if (cursor.value <= 0) {
      cursor.value = -1
      return null
    }
    cursor.value -= 1
    return history.value[cursor.value]
  }

  function resetCursor() {
    cursor.value = -1
  }

  function saveDraft(text: string) {
    pendingDraft.value = text
  }

  watchDebounced(pendingDraft, (val) => { draftStorage.value = val }, { debounce: 500 })

  function loadDraft(): string {
    return draftStorage.value
  }

  return { push, prev, next, resetCursor, saveDraft, loadDraft }
}
