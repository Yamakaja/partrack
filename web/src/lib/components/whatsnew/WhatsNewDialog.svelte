<script lang="ts">
  import { Dialog } from 'bits-ui';
  import { Sparkles, X } from 'lucide-svelte';
  import { whatsNewStore, dismissWhatsNew } from '$lib/stores/whatsNew.svelte.js';
</script>

<Dialog.Root open={whatsNewStore.open} onOpenChange={(v) => (whatsNewStore.open = v)}>
  <Dialog.Portal>
    <Dialog.Overlay
      class="fixed inset-0 z-50 bg-black/50 backdrop-blur-sm data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
    />
    <Dialog.Content
      class="fixed left-1/2 top-1/2 z-50 flex max-h-[80vh] w-full max-w-lg -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-border bg-background shadow-lg duration-200 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95"
    >
      <div class="flex items-center justify-between border-b border-border px-5 py-3">
        <Dialog.Title class="flex items-center gap-2 text-base font-semibold">
          <Sparkles class="size-4 text-primary" />
          What's new
        </Dialog.Title>
        <Dialog.Close
          class="text-muted-foreground hover:text-foreground rounded p-1 hover:bg-accent/50"
          aria-label="Close"
        >
          <X class="size-4" />
        </Dialog.Close>
      </div>
      <Dialog.Description class="sr-only">Changes since you last used partrack</Dialog.Description>

      <div class="flex-1 space-y-5 overflow-y-auto px-5 py-4">
        {#each whatsNewStore.releases as release (release.version)}
          <section>
            <div class="mb-2 flex items-baseline gap-2">
              <h3 class="text-sm font-semibold">{release.version}</h3>
              <span class="text-xs text-muted-foreground">{release.date}</span>
            </div>
            <ul class="space-y-1.5">
              {#each release.entries as entry}
                <li class="flex gap-2 text-sm">
                  <span class="mt-1.5 size-1.5 shrink-0 rounded-full bg-primary/70"></span>
                  <span>{entry}</span>
                </li>
              {/each}
            </ul>
          </section>
        {/each}
      </div>

      <div class="flex justify-end border-t border-border px-5 py-3">
        <button
          onclick={dismissWhatsNew}
          class="rounded-md bg-primary px-3 py-1.5 text-sm font-medium text-primary-foreground hover:bg-primary/90"
        >
          Got it
        </button>
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
