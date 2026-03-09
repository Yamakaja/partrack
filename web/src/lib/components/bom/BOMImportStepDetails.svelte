<script lang="ts">
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Button } from '$lib/components/ui/button/index.js';

  let {
    label = $bindable(),
    notes = $bindable(),
    canImport,
    importing,
    hasDistributorStep,
    onback,
    onimport,
    oncancel,
  }: {
    label: string;
    notes: string;
    canImport: boolean;
    importing: boolean;
    hasDistributorStep: boolean;
    onback: () => void;
    onimport: () => void;
    oncancel: () => void;
  } = $props();
</script>

<div class="min-h-0 flex-1 overflow-auto px-6 py-4">
  <div class="max-w-xl space-y-3">
    <div class="space-y-1">
      <Label class="text-xs">Revision label</Label>
      <Input bind:value={label} placeholder="e.g. v1.0, Rev A" class="h-8 text-sm max-w-xs" />
    </div>
    <div class="space-y-1">
      <Label class="text-xs">Notes (optional)</Label>
      <textarea
        bind:value={notes}
        rows={3}
        placeholder="Any notes about this revision…"
        class="flex w-full rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm resize-none focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
      ></textarea>
    </div>
  </div>
</div>

<!-- Footer -->
<div class="flex shrink-0 items-center justify-between border-t border-border px-6 py-4">
  <Button variant="outline" onclick={onback}>← Back</Button>
  <div class="flex gap-2">
    <Button variant="outline" onclick={oncancel}>Cancel</Button>
    <Button onclick={onimport} disabled={!canImport || importing}>
      {importing ? 'Importing…' : 'Import'}
    </Button>
  </div>
</div>
