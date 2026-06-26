<template>
  <div class="flex h-dvh flex-col bg-background text-foreground">
    <header class="flex items-center justify-between border-b border-border px-4 py-3">
      <div class="flex items-center gap-2">
        <span class="font-mono text-sm font-semibold tracking-tight">tau</span>
        <span class="text-xs text-muted-foreground">web ui</span>
      </div>
      <slot name="header" />
    </header>

    <main class="min-h-0 flex-1">
      <slot />
    </main>

    <footer class="border-t border-border">
      <slot name="footer" />
    </footer>
  </div>
</template>

<script setup lang="ts"></script>
