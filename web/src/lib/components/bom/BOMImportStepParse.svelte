<script lang="ts">
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { truncateRow } from './bom-import-utils.js';

  let {
    parsedRows,
    headerRow,
    dataRows,
    previewRows,
    delimiter = $bindable(),
    quoteChar = $bindable(),
    escapeChar = $bindable(),
    hasHeader = $bindable(),
    comments = $bindable(),
    skipFirstLines = $bindable(),
    onnext,
    oncancel,
  }: {
    parsedRows: string[][];
    headerRow: string[];
    dataRows: string[][];
    previewRows: string[][];
    delimiter: string;
    quoteChar: string;
    escapeChar: string;
    hasHeader: boolean;
    comments: string;
    skipFirstLines: number;
    onnext: () => void;
    oncancel: () => void;
  } = $props();
</script>

<!-- Parse options -->
<div class="shrink-0 border-b border-border px-6 py-3">
  <div class="flex items-end gap-4">
    <div class="w-20 space-y-1">
      <Label class="text-xs">Delimiter</Label>
      <Input class="h-7 font-mono text-xs" bind:value={delimiter} placeholder="auto" maxlength={3} />
    </div>
    <div class="w-16 space-y-1">
      <Label class="text-xs">Quote</Label>
      <Input class="h-7 font-mono text-xs" bind:value={quoteChar} maxlength={1} />
    </div>
    <div class="w-16 space-y-1">
      <Label class="text-xs">Escape</Label>
      <Input class="h-7 font-mono text-xs" bind:value={escapeChar} maxlength={1} />
    </div>
    <div class="w-16 space-y-1">
      <Label class="text-xs">Comments</Label>
      <Input class="h-7 font-mono text-xs" bind:value={comments} placeholder="none" maxlength={1} />
    </div>
    <div class="w-40 space-y-1">
      <Label class="text-xs">Skip first N lines</Label>
      <input
        type="number"
        min="0"
        bind:value={skipFirstLines}
        class="flex h-7 w-full rounded-md border border-input bg-background px-3 py-1 text-xs shadow-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      />
    </div>
    <div class="flex items-center gap-2 pb-0.5">
      <input
        id="has-header"
        type="checkbox"
        bind:checked={hasHeader}
        class="h-4 w-4 rounded border-input accent-primary"
      />
      <Label for="has-header" class="cursor-pointer text-xs">Header row</Label>
    </div>
  </div>
</div>

<!-- Preview table -->
<div class="min-h-0 flex-1 overflow-auto px-6 py-4">
  {#if parsedRows.length === 0}
    <p class="py-8 text-center text-sm text-muted-foreground">
      No data parsed — check parse options above.
    </p>
  {:else}
    <div class="overflow-x-auto">
      <table class="w-max border-collapse text-xs">
        <thead>
          <tr class="border-b border-border">
            {#each headerRow as h}
              <th class="whitespace-nowrap bg-muted/40 px-3 py-1.5 text-left font-semibold text-muted-foreground">
                {h}
              </th>
            {/each}
          </tr>
        </thead>
        <tbody>
          {#each previewRows as row, i (i)}
            <tr class="border-b border-border/60 hover:bg-muted/20">
              {#each headerRow as _, ci}
                <td class="whitespace-nowrap px-3 py-1.5 font-mono text-muted-foreground">
                  {truncateRow(row[ci])}
                </td>
              {/each}
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    {#if dataRows.length > 20}
      <p class="mt-2 text-xs text-muted-foreground">
        Showing first 20 of {dataRows.length} data rows.
      </p>
    {/if}
  {/if}
</div>

<!-- Footer -->
<div class="flex shrink-0 justify-end gap-2 border-t border-border px-6 py-4">
  <Button variant="outline" onclick={oncancel}>Cancel</Button>
  <Button onclick={onnext} disabled={parsedRows.length === 0}>
    Next: Map Columns →
  </Button>
</div>
