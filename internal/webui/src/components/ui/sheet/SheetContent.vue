<script setup lang="ts">
import { XIcon } from '@lucide/vue'
import type { DialogContentEmits, DialogContentProps } from 'reka-ui'
import type { HTMLAttributes } from 'vue'
import { reactiveOmit } from '@vueuse/core'
import {
  DialogClose,
  DialogContent,
  DialogOverlay,
  DialogPortal,
  useForwardPropsEmits,
} from 'reka-ui'
import { cn } from '@/lib/utils'

defineOptions({ inheritAttrs: false })

const props = withDefaults(
  defineProps<DialogContentProps & { class?: HTMLAttributes['class']; side?: 'right' | 'left' }>(),
  { side: 'right' },
)
const emits = defineEmits<DialogContentEmits>()

const delegatedProps = reactiveOmit(props, 'class', 'side')
const forwarded = useForwardPropsEmits(delegatedProps, emits)
</script>

<template>
  <DialogPortal>
    <DialogOverlay
      class="fixed inset-0 z-50 bg-black/60 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
    />
    <DialogContent
      data-slot="sheet-content"
      v-bind="{ ...$attrs, ...forwarded }"
      :class="
        cn(
          'bg-popover text-popover-foreground fixed z-50 flex h-full w-full max-w-sm flex-col gap-4 border-l border-border p-6 shadow-lg outline-none transition-transform duration-200 ease-in-out',
          'data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:duration-200 data-[state=open]:duration-200',
          side === 'right'
            ? 'inset-y-0 right-0 data-[state=closed]:slide-out-to-right data-[state=open]:slide-in-from-right'
            : 'inset-y-0 left-0 border-l-0 border-r border-border data-[state=closed]:slide-out-to-left data-[state=open]:slide-in-from-left',
          props.class,
        )
      "
    >
      <slot />

      <DialogClose
        class="absolute top-4 right-4 rounded-md p-1 text-muted-foreground opacity-70 transition hover:opacity-100 focus:outline-none focus:ring-1 focus:ring-ring"
      >
        <XIcon class="size-4" />
        <span class="sr-only">Close</span>
      </DialogClose>
    </DialogContent>
  </DialogPortal>
</template>
