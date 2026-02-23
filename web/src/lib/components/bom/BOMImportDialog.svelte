<script lang="ts">
  import Papa  from 'papaparse';
  import { Dialog } from 'bits-ui';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { importRevision } from '$lib/api/revisions.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';

  type ColumnRole = 'ignore' | 'designators' | 'quantity' | 'mpn' | 'description'
               | 'manufacturer' | 'name' | 'sku' | 'distributor';

  let {
    teamId,
    projectId,
    oncomplete,
    trigger,
  }: {
    teamId: string;
    projectId: string;
    oncomplete: (jobId: string, revisionId: string) => void;
    trigger: import('svelte').Snippet<[() => void]>;
  } = $props();

  const AUTO_MAP: Record<string, ColumnRole> = {
    ref: 'designators',
    reference: 'designators',
    refdes: 'designators',
    designator: 'designators',
    designators: 'designators',
    refs: 'designators',
    qty: 'quantity',
    quantity: 'quantity',
    count: 'quantity',
    mpn: 'mpn',
    'manufacturer part number': 'mpn',
    'manufacturer part number 1': 'mpn',
    'part number': 'mpn',
    partnumber: 'mpn',
    part_number: 'mpn',
    description: 'description',
    desc: 'description',
    value: 'description',
    manufacturer: 'manufacturer',
    'manufacturer 1': 'manufacturer',
    mfr: 'manufacturer',
    mfg: 'manufacturer',
    'manufacturer name': 'manufacturer',
    name: 'name',
    sku: 'sku',
    'distributor sku': 'sku',
    'supplier sku': 'sku',
    distributor: 'distributor',
    'distributor name': 'distributor',
    vendor: 'distributor',
    supplier: 'distributor',
    'supplier 1': 'distributor',
    'supplier part number 1': 'sku'
  };

  const ROLE_LABELS: Record<ColumnRole, string> = {
    ignore: '— Ignore —',
    designators: 'Designators *',
    quantity: 'Quantity *',
    mpn: 'MPN',
    description: 'Description',
    manufacturer: 'Manufacturer',
    name: 'Name',
    sku: 'SKU',
    distributor: 'Distributor',
  };

  const COLUMN_WIDTH: number = 80

  let open = $state(false);
  let step = $state<'parse' | 'map'>('parse');
  let rawText = $state('');
  let parsedRows = $state<string[][]>([]);

  // Parse options
  let delimiter = $state('');
  let quoteChar = $state('"');
  let escapeChar = $state('"');
  let hasHeader = $state(true);
  let comments = $state('');
  let skipFirstLines = $state(0);

  // Column mapping — one ColumnRole per column
  let columnRoles = $state<ColumnRole[]>([]);

  // Revision details
  let label = $state('');
  let importing = $state(false);

  let fileInputEl: HTMLInputElement;

  // Re-parse whenever any option or raw text changes.
  $effect(() => {
    // Reference all parse-option reactive vars to establish dependencies.
    // eslint-disable-next-line @typescript-eslint/no-unused-expressions
    delimiter; quoteChar; escapeChar; hasHeader; comments; skipFirstLines; rawText;
    if (rawText) reparse();
  });

  function openFilePicker() {
    fileInputEl.value = ''; // allow re-selecting the same file
    fileInputEl.click();
  }

  async function handleFile(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (!file) return;

    // Reset all state for a fresh import
    delimiter = '';
    quoteChar = '"';
    escapeChar = '"';
    hasHeader = true;
    comments = '';
    skipFirstLines = 0;
    label = '';
    columnRoles = [];
    step = 'parse';

    rawText = await file.text();
    open = true;
  }

  function reparse() {
    let text = rawText;
    if (skipFirstLines > 0) {
      const lines = text.split(/\r?\n/);
      text = lines.slice(skipFirstLines).join('\n');
    }
    
    const result = Papa.parse<string[]>(text, {
      delimiter: delimiter,
      quoteChar: quoteChar,
      escapeChar: escapeChar,
      header: false,
      comments: comments || false,
      skipEmptyLines: true,
      dynamicTyping: false
    });
    
    parsedRows = result.data as string[][];
  }

  function advanceToMap() {
    const numCols = parsedRows[0]?.length ?? 0;
    if (hasHeader && parsedRows.length > 0) {
      columnRoles = parsedRows[0].map(h => AUTO_MAP[h.toLowerCase().trim()] ?? 'ignore');
    } else {
      columnRoles = Array.from({ length: numCols }, () => 'ignore' as ColumnRole);
    }
    step = 'map';
  }

  function handleClose() {
    open = false;
  }

  // Derived views of the parsed data
  let headerRow = $derived(
    hasHeader && parsedRows.length > 0
      ? parsedRows[0]
      : (parsedRows[0]?.map((_, i) => `Column ${i + 1}`) ?? [])
  );
  let dataRows = $derived(hasHeader ? parsedRows.slice(1) : parsedRows);
  let previewRows = $derived(dataRows.slice(0, 20));

  let canImport = $derived(
    label.trim().length > 0 &&
    columnRoles.filter(r => r === 'designators').length === 1 &&
    columnRoles.filter(r => r === 'quantity').length === 1
  );

  async function handleImport() {
    if (!canImport) return;
    importing = true;
    try {
      const desigIdx = columnRoles.indexOf('designators');
      const qtyIdx   = columnRoles.indexOf('quantity');
      const descIdx  = columnRoles.indexOf('description');
      const mfrIdx   = columnRoles.indexOf('manufacturer');
      const mpnIdx   = columnRoles.indexOf('mpn');
      const nameIdx  = columnRoles.indexOf('name');
      const skuIdx   = columnRoles.indexOf('sku');
      const distIdx  = columnRoles.indexOf('distributor');

      const items = dataRows
        .map(row => ({
          reference_designators: row[desigIdx]?.trim() ?? '',
          quantity: parseInt(row[qtyIdx] ?? '0', 10),
          raw_description:  descIdx >= 0 && row[descIdx]?.trim() ? row[descIdx].trim() : undefined,
          mpn:              mpnIdx >= 0  && row[mpnIdx]?.trim()  ? row[mpnIdx].trim()  : undefined,
          manufacturer:     mfrIdx  >= 0 && row[mfrIdx]?.trim()  ? row[mfrIdx].trim()  : undefined,
          name:             nameIdx >= 0 && row[nameIdx]?.trim() ? row[nameIdx].trim() : undefined,
          sku:              skuIdx  >= 0 && row[skuIdx]?.trim()  ? row[skuIdx].trim()  : undefined,
          distributor_name: distIdx >= 0 && row[distIdx]?.trim() ? row[distIdx].trim() : undefined,
        }))
        .filter(it => it.reference_designators && it.quantity > 0);

      const result = await importRevision(teamId, projectId, {
        label: label.trim(),
        set_active: true,
        bom_import_config: {
          delimiter,
          quoteChar,
          escapeChar,
          hasHeader: String(hasHeader),
          comments,
          skipFirstLines: String(skipFirstLines),
          ref_col:          desigIdx >= 0 ? (headerRow[desigIdx] ?? '') : '',
          qty_col:          qtyIdx   >= 0 ? (headerRow[qtyIdx]   ?? '') : '',
          mpn_col:          mpnIdx   >= 0 ? (headerRow[mpnIdx]   ?? '') : '',
          description_col:  descIdx  >= 0 ? (headerRow[descIdx]  ?? '') : '',
          manufacturer_col: mfrIdx   >= 0 ? (headerRow[mfrIdx]   ?? '') : '',
          name_col:         nameIdx  >= 0 ? (headerRow[nameIdx]  ?? '') : '',
          sku_col:          skuIdx   >= 0 ? (headerRow[skuIdx]   ?? '') : '',
          distributor_col:  distIdx  >= 0 ? (headerRow[distIdx]  ?? '') : '',
        },
        items,
      });

      open = false;
      pushToast('info', 'BOM import started');
      oncomplete(result.job_id, result.bom_revision_id);
    } catch (err: any) {
      pushToast('error', 'Import failed', err.message);
    } finally {
      importing = false;
    }
  }

  function truncateRow(s: string|undefined): string {
    if (!s)
      return "";
    if (s.length <= COLUMN_WIDTH)
      return s;

    return s.substring(0, COLUMN_WIDTH) + "..."
  }

  function checkForDuplicates(roles: ColumnRole[]): boolean {
    let counter: Record<string, boolean> = {}
    for (let i = 0; i < roles.length; i++) {
      if (roles[i] === "ignore")
        continue;

      if (counter[roles[i]])
        return true;

      counter[roles[i]] = true;
    }

    return false;
  }
</script>

<!-- Hidden file input — triggered programmatically -->
<input
  bind:this={fileInputEl}
  type="file"
  accept=".csv"
  class="hidden"
  onchange={handleFile}
/>

<!-- Render the trigger slot, passing our openFilePicker handler -->
{@render trigger(openFilePicker)}

<Dialog.Root bind:open>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content
      class="fixed left-1/2 top-1/2 z-50 flex w-[90vw] -translate-x-1/2 -translate-y-1/2 flex-col rounded-lg border border-border bg-background shadow-lg"
      style="max-height: 85vh;"
    >
      <!-- Dialog header -->
      <div class="flex shrink-0 items-start justify-between border-b border-border px-6 pb-4 pt-5">
        <div>
          <Dialog.Title class="text-lg font-semibold">Import BOM</Dialog.Title>
          <Dialog.Description class="mt-0.5 text-sm text-muted-foreground">
            {#if step === 'parse'}
              Adjust parsing options and verify the preview, then proceed to column mapping.
            {:else}
              Assign each column a role, enter a revision label, then click Import.
            {/if}
          </Dialog.Description>
        </div>
        <div class="ml-4 flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground">
          <span class={step === 'parse' ? 'font-semibold text-foreground' : ''}>1. Parse</span>
          <span>›</span>
          <span class={step === 'map' ? 'font-semibold text-foreground' : ''}>2. Map Columns</span>
        </div>
      </div>

      {#if step === 'parse'}
        <!-- ── Step 1: Parse options + preview ── -->

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

        <!-- Step 1 footer -->
        <div class="flex shrink-0 justify-end gap-2 border-t border-border px-6 py-4">
          <Button variant="outline" onclick={handleClose}>Cancel</Button>
          <Button onclick={advanceToMap} disabled={parsedRows.length === 0}>
            Next: Map Columns →
          </Button>
        </div>

      {:else}
        <!-- ── Step 2: Column mapping + revision label ── -->

        <div class="min-h-0 flex-1 overflow-auto px-6 py-4">
          {#if headerRow.length === 0}
            <p class="py-8 text-center text-sm text-muted-foreground">No columns to map.</p>
          {:else}
            <div class="overflow-x-auto">
              <table class="w-max border-collapse text-xs">
                <thead>
                  <tr class="border-b-2 border-border">
                    {#each headerRow as colName, ci}
                      <th class="min-w-[140px] px-2 py-2 align-top">
                        <select
                          bind:value={columnRoles[ci]}
                          class="w-full rounded border border-input bg-background px-1.5 py-0.5 text-xs focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                        >
                          {#each Object.entries(ROLE_LABELS) as [role, roleLabel]}
                            <option value={role}>{roleLabel}</option>
                          {/each}
                        </select>
                        {#if hasHeader}
                          <div class="mt-0.5 max-w-[130px] truncate text-muted-foreground" title={colName}>
                            {colName}
                          </div>
                        {/if}
                      </th>
                    {/each}
                  </tr>
                </thead>
                <tbody>
                  {#each previewRows as row, i (i)}
                    <tr class="border-b border-border/60 hover:bg-muted/20">
                      {#each headerRow as _, ci}
                        <td class="whitespace-nowrap px-2 py-1.5 font-mono text-muted-foreground">
                          {truncateRow(row[ci])}
                        </td>
                      {/each}
                    </tr>
                  {/each}
                </tbody>
              </table>
            </div>
          {/if}

          <!-- Revision label -->
          <div class="mt-5 max-w-xs space-y-1">
            <Label for="bom-label">
              Revision label <span class="text-destructive">*</span>
            </Label>
            <Input id="bom-label" bind:value={label} placeholder="e.g. v1.0, Rev A" />
          </div>

          {#if checkForDuplicates(columnRoles)}
            <p class="mt-2 text-xs text-destructive">
              Each column can only be assigned once.
            </p>
          {:else if columnRoles.filter(r => r === 'designators').length === 0 || columnRoles.filter(r => r === 'quantity').length === 0}
            <p class="mt-2 text-xs text-muted-foreground">
              Assign at least one column as <strong>Designators *</strong> and one as <strong>Quantity *</strong> to enable import.
            </p>
          {/if}
        </div>

        <!-- Step 2 footer -->
        <div class="flex shrink-0 items-center justify-between border-t border-border px-6 py-4">
          <Button variant="outline" onclick={() => (step = 'parse')}>← Back</Button>
          <div class="flex gap-2">
            <Button variant="outline" onclick={handleClose}>Cancel</Button>
            <Button onclick={handleImport} disabled={!canImport || importing}>
              {importing ? 'Importing…' : 'Import'}
            </Button>
          </div>
        </div>
      {/if}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
