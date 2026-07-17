<script lang="ts">
  import { X, StickyNote } from 'lucide-svelte';
  import PartCell from '$lib/components/parts/PartCell.svelte';
  import type { CollectionItem } from '$lib/api/collections.js';

  // One list row: two-line part cell, inline-editable quantity and note,
  // delete. Quantity NULL renders as a warning "—" — quantitative verbs skip
  // those rows, so the row is where the user fixes them.
  let {
    teamId,
    item,
    onupdate,
    ondelete,
  }: {
    teamId: string;
    item: CollectionItem;
    onupdate: (itemId: string, body: { quantity?: number | null; note?: string | null }) => Promise<void>;
    ondelete: (itemId: string) => Promise<void>;
  } = $props();

  let editingQty = $state(false);
  let qtyValue = $state('');
  let editingNote = $state(false);
  let noteValue = $state('');

  function startQtyEdit() {
    qtyValue = item.quantity != null ? String(item.quantity) : '';
    editingQty = true;
  }

  async function saveQty() {
    if (!editingQty) return;
    editingQty = false;
    const trimmed = qtyValue.trim();
    const next = trimmed === '' ? null : Number.parseInt(trimmed, 10);
    if (next !== null && (!Number.isFinite(next) || next <= 0)) return;
    if (next === item.quantity) return;
    await onupdate(item.id, { quantity: next });
  }

  function startNoteEdit() {
    noteValue = item.note ?? '';
    editingNote = true;
  }

  async function saveNote() {
    if (!editingNote) return;
    editingNote = false;
    const next = noteValue.trim() === '' ? null : noteValue.trim();
    if (next === item.note) return;
    await onupdate(item.id, { note: next });
  }

  function qtyKey(e: KeyboardEvent) {
    if (e.key === 'Enter') { e.preventDefault(); saveQty(); }
    else if (e.key === 'Escape') editingQty = false;
  }

  function noteKey(e: KeyboardEvent) {
    if (e.key === 'Enter') { e.preventDefault(); saveNote(); }
    else if (e.key === 'Escape') editingNote = false;
  }

  function focusOnMount(el: HTMLInputElement) {
    el.focus();
    el.select();
  }
</script>

<div class="group border-b border-border px-3 py-2">
  <div class="grid grid-cols-[minmax(0,1fr)_64px_auto] items-center gap-2">
    <PartCell
      partId={item.part_id}
      mpn={item.mpn}
      {teamId}
      manufacturer={item.manufacturer}
      description={item.description}
      lifecycle={item.lifecycle_status}
      imageUrl={item.image_url}
      availableQty={item.available_qty}
      neededQty={item.quantity}
      stockRingClass="ring-card"
    />
    <div class="text-right">
      {#if editingQty}
        <input
          use:focusOnMount
          bind:value={qtyValue}
          onkeydown={qtyKey}
          onblur={() => (editingQty = false)}
          inputmode="numeric"
          class="w-full rounded border border-input bg-background px-1.5 py-0.5 text-right text-xs tabular-nums focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        />
      {:else}
        <button
          onclick={startQtyEdit}
          title={item.quantity == null ? 'No quantity — click to set' : 'Edit quantity'}
          class="w-full rounded px-1.5 py-0.5 text-right text-xs tabular-nums hover:bg-accent/60
            {item.quantity == null ? 'text-warning font-medium' : ''}"
        >
          {item.quantity != null ? item.quantity.toLocaleString() : '—'}
        </button>
      {/if}
    </div>
    <!-- Both icons always occupy space; the note toggle reveals via opacity so
         rows never change size on hover (hover cards need a stable cursor target). -->
    <div class="flex items-center gap-1">
      <button
        onclick={startNoteEdit}
        title={item.note ? 'Edit note' : 'Add note'}
        class="text-muted-foreground/60 opacity-0 transition-opacity hover:text-foreground focus-visible:opacity-100 group-hover:opacity-100 {editingNote ? 'opacity-100' : ''}"
      >
        <StickyNote class="h-3.5 w-3.5" />
      </button>
      <button
        onclick={() => ondelete(item.id)}
        title="Remove from list"
        class="text-muted-foreground/50 hover:text-destructive"
      >
        <X class="h-3.5 w-3.5" />
      </button>
    </div>
  </div>

  {#if editingNote}
    <input
      use:focusOnMount
      bind:value={noteValue}
      onkeydown={noteKey}
      onblur={saveNote}
      placeholder="Note…"
      class="mt-1 w-full rounded border border-input bg-background px-1.5 py-0.5 text-xs italic focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    />
  {:else if item.note}
    <button
      onclick={startNoteEdit}
      class="mt-0.5 block w-full truncate rounded px-1.5 text-left text-[11px] italic text-muted-foreground hover:bg-accent/60"
      title="Edit note"
    >
      {item.note}
    </button>
  {/if}
</div>
