<script lang="ts" module>
  export interface PickerRow {
    id: string;
    label: string;
    sublabel?: string;
  }
</script>

<script lang="ts">
  import { Dialog } from 'bits-ui';
  import { Button } from '$lib/components/ui/button/index.js';

  // Generic single-pick dialog for the list panel's off-page source verbs
  // ("Add from a build/order/list…"): the caller fetches the rows, this just
  // renders and returns the chosen id.
  let {
    open = $bindable(false),
    title,
    emptyText,
    rows,
    loading = false,
    onpick,
  }: {
    open?: boolean;
    title: string;
    emptyText: string;
    rows: PickerRow[];
    loading?: boolean;
    onpick: (id: string) => void;
  } = $props();
</script>

<Dialog.Root bind:open>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-4 shadow-lg">
      <Dialog.Title class="mb-3 text-lg font-semibold">{title}</Dialog.Title>
      <div class="-mx-1 max-h-72 overflow-y-auto">
        {#if loading}
          <p class="px-1 py-3 text-sm text-muted-foreground">Loading…</p>
        {:else if rows.length === 0}
          <p class="px-1 py-3 text-sm text-muted-foreground">{emptyText}</p>
        {:else}
          {#each rows as row (row.id)}
            <button
              onclick={() => { open = false; onpick(row.id); }}
              class="flex w-full flex-col items-start rounded px-2 py-1.5 text-left hover:bg-accent"
            >
              <span class="w-full truncate text-sm">{row.label}</span>
              {#if row.sublabel}
                <span class="w-full truncate text-xs text-muted-foreground">{row.sublabel}</span>
              {/if}
            </button>
          {/each}
        {/if}
      </div>
      <div class="mt-3 flex justify-end">
        <Dialog.Close>
          <Button variant="outline" type="button">Cancel</Button>
        </Dialog.Close>
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
