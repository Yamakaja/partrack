<script lang="ts">
  import Papa from 'papaparse';
  import { Dialog } from 'bits-ui';
  import { importRevision, interpretPreview, type BOMImportConfig, type Interpretation } from '$lib/api/revisions.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import type { Distributor } from '$lib/types/parts.js';
  import { listDistributors } from '$lib/api/distributors.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { AUTO_MAP, buildItems, type BOMImportItem, type ColumnRole, type DistMapping } from './bom-import-utils.js';
  import BOMImportStepParse from './BOMImportStepParse.svelte';
  import BOMImportStepMapColumns from './BOMImportStepMapColumns.svelte';
  import BOMImportStepMapDistributors from './BOMImportStepMapDistributors.svelte';
  import BOMImportStepInterpret from './BOMImportStepInterpret.svelte';
  import BOMImportStepDetails from './BOMImportStepDetails.svelte';

  let {
    teamId,
    projectId,
    // The parent should pass the current project's saved BOM import config
    // (projects.bom_import_config) so we can pre-populate the dialog.
    initialConfig,
    oncomplete,
    trigger,
  }: {
    teamId: string;
    projectId: string;
    initialConfig?: BOMImportConfig | null;
    oncomplete?: (jobId: string, revisionId: string) => void;
    trigger: import('svelte').Snippet<[() => void]>;
  } = $props();

  let open = $state(false);
  let step = $state<'parse' | 'map' | 'distributor-map' | 'interpret' | 'details'>('parse');
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

  // Distributor mapping step
  let uniqueDistributorNames = $state<string[]>([]);
  let distributorMappings = $state<DistMapping>({});
  let availableDistributors = $state<Distributor[]>([]);
  // 'none' = skip step, 'multi' = per-name mapping, 'single' = one distributor for all SKUs.
  let distributorStepMode = $state<'none' | 'multi' | 'single'>('none');
  let singleDistributorId = $state('');

  // Interpretation review step
  let interpItems = $state<BOMImportItem[]>([]);
  let interpResults = $state<Interpretation[]>([]);
  let interpEdited = $state<Record<number, boolean>>({});
  let interpLoading = $state(false);
  // Async LLM read: correlation id + the row indices the AI is still reading.
  let interpJobId = $state('');
  let interpPending = $state<number[]>([]);

  // Revision details
  let label = $state('');
  let notes = $state('');
  let importing = $state(false);

  let fileInputEl: HTMLInputElement;

  // Holds the last loaded bom_import_config so that advanceToMap can apply role
  // mappings based on stored header names.
  let _storedConfigForMapping: BOMImportConfig | null | undefined = null;

  // Initialize parse + mapping options from a previously saved bom_import_config.
  function applyInitialConfig(config: BOMImportConfig | null | undefined) {
    if (!config) return;

    if (config.delimiter) delimiter = config.delimiter;
    if (config.quoteChar) quoteChar = config.quoteChar;
    if (config.escapeChar) escapeChar = config.escapeChar;
    if (config.comments) comments = config.comments;

    if (config.hasHeader !== undefined) {
      hasHeader = config.hasHeader === 'true' || config.hasHeader === '1';
    }

    if (config.skipFirstLines !== undefined) {
      const n = parseInt(config.skipFirstLines, 10);
      if (!Number.isNaN(n) && n >= 0) {
        skipFirstLines = n;
      }
    }

    // Column mapping based on stored header names is applied during advanceToMap.
    _storedConfigForMapping = config;
    distributorMappings = config.mapping ?? {};
    singleDistributorId = config.single_distributor_id ?? '';
  }

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
    notes = '';
    columnRoles = [];
    distributorStepMode = 'none';
    uniqueDistributorNames = [];
    step = 'parse';

    // Re-apply the project's saved import config (overrides defaults above).
    applyInitialConfig(initialConfig);

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
      dynamicTyping: false,
    });

    parsedRows = result.data as string[][];
  }

  function advanceToMap() {
    const numCols = parsedRows[0]?.length ?? 0;

    if (hasHeader && parsedRows.length > 0) {
      columnRoles = parsedRows[0].map((h) => AUTO_MAP[h.toLowerCase().trim()] ?? 'ignore');
    } else {
      columnRoles = Array.from({ length: numCols }, () => 'ignore' as ColumnRole);
    }

    // If we have a stored config and a header row, refine the mapping so the
    // exact columns the user picked last time are re-selected.
    if (hasHeader && parsedRows.length > 0 && _storedConfigForMapping) {
      const header = parsedRows[0];

      const tryAssignRoleFromConfig = (configKey: keyof BOMImportConfig, role: ColumnRole) => {
        const colName = _storedConfigForMapping?.[configKey] as string | undefined;
        if (!colName) return;
        const idx = header.findIndex((h) => h === colName);
        if (idx >= 0) {
          columnRoles[idx] = role;
        }
      };

      tryAssignRoleFromConfig('ref_col', 'designators');
      tryAssignRoleFromConfig('qty_col', 'quantity');
      tryAssignRoleFromConfig('mpn_col', 'mpn');
      tryAssignRoleFromConfig('description_col', 'description');
      tryAssignRoleFromConfig('manufacturer_col', 'manufacturer');
      tryAssignRoleFromConfig('name_col', 'name');
      tryAssignRoleFromConfig('sku_col', 'sku');
      tryAssignRoleFromConfig('distributor_col', 'distributor');
      tryAssignRoleFromConfig('value_col', 'value');
      tryAssignRoleFromConfig('footprint_col', 'footprint');
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
      : (parsedRows[0]?.map((_, i) => `Column ${i + 1}`) ?? []),
  );
  let dataRows = $derived(hasHeader ? parsedRows.slice(1) : parsedRows);
  let previewRows = $derived(dataRows.slice(0, 20));

  // Column requirements (designators + quantity must each be mapped exactly once)
  let hasRequiredColumns = $derived(
    columnRoles.filter((r) => r === 'designators').length === 1 &&
      columnRoles.filter((r) => r === 'quantity').length === 1,
  );

  // Final import requires a revision label and valid column mapping.
  let canImport = $derived(label.trim().length > 0 && hasRequiredColumns);

  async function handleMapStepNext() {
    if (!hasRequiredColumns || importing) return;

    const items = buildItems(dataRows, columnRoles);
    const names = Array.from(
      new Set(
        items
          .map((it) => it.distributor_name?.trim())
          .filter((v): v is string => !!v && v.length > 0),
      ),
    );

    const hasSkuColumn = columnRoles.includes('sku');
    const hasDistributorColumn = columnRoles.includes('distributor');

    if (names.length > 0) {
      distributorStepMode = 'multi';
      uniqueDistributorNames = names;
      for (const n of names) {
        if (!(n in distributorMappings)) distributorMappings[n] = undefined;
      }
    } else if (hasSkuColumn && !hasDistributorColumn) {
      // SKUs present but no distributor column — ask for a single distributor.
      distributorStepMode = 'single';
      uniqueDistributorNames = [];
    } else {
      // Nothing to map — go straight to interpretation review.
      distributorStepMode = 'none';
      await enterInterpretStep();
      return;
    }

    importing = true;
    try {
      const resp = await listDistributors();
      availableDistributors = resp.items;
      step = 'distributor-map';
    } catch (err: any) {
      pushToast('error', 'Failed to load distributors', err.message ?? String(err));
    } finally {
      importing = false;
    }
  }

  async function handleDistributorNext() {
    await enterInterpretStep();
  }

  // Build the final items (with the chosen distributor mapping) and fetch the
  // parser's interpretation for the review gate.
  async function enterInterpretStep() {
    const single =
      distributorStepMode === 'single' && singleDistributorId
        ? { id: singleDistributorId, name: availableDistributors.find((d) => d.id === singleDistributorId)?.name ?? '' }
        : undefined;
    const mapping = distributorStepMode === 'multi' ? distributorMappings : undefined;
    interpItems = buildItems(dataRows, columnRoles, mapping, single, headerRow);
    interpResults = [];
    interpEdited = {};
    interpJobId = '';
    interpPending = [];
    step = 'interpret';
    interpLoading = true;
    try {
      // stream:true → Phase-1 readings return immediately; LLM rows stream in over SSE
      // (handled live in BOMImportStepInterpret), so the list shows without a long wait.
      const resp = await interpretPreview(
        interpItems.map((it) => ({
          refdes: it.reference_designators,
          footprint: it.footprint,
          value: it.value,
          mpn: it.mpn,
          sku: it.sku,
          row: it.row,
        })),
        teamId,
        projectId,
        { stream: true },
      );
      interpResults = resp.items;
      interpJobId = resp.job_id ?? '';
      interpPending = resp.pending ?? [];
    } catch (err: any) {
      pushToast('error', 'Failed to interpret rows', err.message ?? String(err));
    } finally {
      interpLoading = false;
    }
  }

  async function performImport(mapping?: DistMapping) {
    if (!canImport) return;
    importing = true;
    try {
      // Items were built (and reviewed) when entering the interpret step. Attach a
      // reviewer-confirmed interpretation for rows the user edited ('user') or the
      // LLM read ('llm'); both are persisted as overrides. Drop the raw `row` — only
      // the preview needs it, and it would bloat the import payload.
      const items = interpItems.map((it, i) => {
        const { row: _row, ...rest } = it;
        const r = interpResults[i];
        if (interpEdited[i] && r) return { ...rest, interpretation: r, interpretation_source: 'user' };
        if (r?.source === 'llm') return { ...rest, interpretation: r, interpretation_source: 'llm' };
        return rest;
      });

      const desigIdx = columnRoles.indexOf('designators');
      const qtyIdx   = columnRoles.indexOf('quantity');
      const descIdx  = columnRoles.indexOf('description');
      const mfrIdx   = columnRoles.indexOf('manufacturer');
      const mpnIdx   = columnRoles.indexOf('mpn');
      const nameIdx  = columnRoles.indexOf('name');
      const skuIdx   = columnRoles.indexOf('sku');
      const distIdx  = columnRoles.indexOf('distributor');
      const valueIdx = columnRoles.indexOf('value');
      const fpIdx    = columnRoles.indexOf('footprint');

      const result = await importRevision(teamId, projectId, {
        label: label.trim(),
        notes: notes.trim() || undefined,
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
          value_col:        valueIdx >= 0 ? (headerRow[valueIdx] ?? '') : '',
          footprint_col:    fpIdx    >= 0 ? (headerRow[fpIdx]    ?? '') : '',
          mapping: mapping,
          single_distributor_id: distributorStepMode === 'single' ? singleDistributorId : undefined,
        },
        items,
      });

      open = false;
      const revPath = `/teams/${teamId}/projects/${projectId}/revisions/${result.bom_revision_id}`;
      navigate(revPath);
      history.replaceState(null, '', `${revPath}?import_job=${result.job_id}`);
    } catch (err: any) {
      pushToast('error', 'Import failed', err.message ?? String(err));
    } finally {
      importing = false;
    }
  }

  async function handleFinalImport() {
    if (!canImport || importing) return;
    const mapping = distributorStepMode === 'multi' ? distributorMappings : undefined;
    await performImport(mapping);
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
      style="height: min(85vh, 800px);"
    >
      <!-- Dialog header -->
      <div class="flex shrink-0 items-center justify-between border-b border-border px-6 pb-4 pt-5 gap-6">
        <!-- Left: title + description -->
        <div class="space-y-1">
          <Dialog.Title class="text-lg font-semibold">Import BOM</Dialog.Title>
          <Dialog.Description class="text-sm text-muted-foreground">
            {#if step === 'parse'}
              Adjust parsing options and verify the preview, then proceed to column mapping.
            {:else if step === 'map'}
              Assign each column a role, then continue to distributor mapping or import.
            {:else if step === 'distributor-map'}
              Map each distributor name from the CSV to a known distributor, then continue.
            {:else if step === 'interpret'}
              Review how each row was interpreted. Fix anything wrong, then continue — most rows need no changes.
            {:else}
              Review the revision label and confirm the import.
            {/if}
          </Dialog.Description>
        </div>

        <!-- Right: step indicator -->
        <div class="flex shrink-0 items-center gap-1.5 text-xs text-muted-foreground">
          <span class={step === 'parse' ? 'font-semibold text-foreground' : ''}>1. Parse</span>
          <span>›</span>
          <span class={step === 'map' ? 'font-semibold text-foreground' : ''}>2. Map Columns</span>
          <span>›</span>
          <span class={step === 'distributor-map' ? 'font-semibold text-foreground' : ''}>3. Map Distributors</span>
          <span>›</span>
          <span class={step === 'interpret' ? 'font-semibold text-foreground' : ''}>4. Interpret</span>
          <span>›</span>
          <span class={step === 'details' ? 'font-semibold text-foreground' : ''}>5. Details</span>
        </div>
      </div>

      {#if step === 'parse'}
        <BOMImportStepParse
          {parsedRows}
          {headerRow}
          {dataRows}
          {previewRows}
          bind:delimiter
          bind:quoteChar
          bind:escapeChar
          bind:hasHeader
          bind:comments
          bind:skipFirstLines
          onnext={advanceToMap}
          oncancel={handleClose}
        />
      {:else if step === 'map'}
        <BOMImportStepMapColumns
          {headerRow}
          {previewRows}
          {hasHeader}
          bind:columnRoles
          {hasRequiredColumns}
          {importing}
          onback={() => (step = 'parse')}
          onnext={handleMapStepNext}
          oncancel={handleClose}
        />
      {:else if step === 'distributor-map'}
        <BOMImportStepMapDistributors
          {uniqueDistributorNames}
          {availableDistributors}
          bind:distributorMappings
          singleMode={distributorStepMode === 'single'}
          bind:singleDistributorId
          {hasRequiredColumns}
          onback={() => (step = 'map')}
          onnext={handleDistributorNext}
          oncancel={handleClose}
        />
      {:else if step === 'interpret'}
        <BOMImportStepInterpret
          bind:items={interpItems}
          bind:interpretations={interpResults}
          bind:edited={interpEdited}
          {teamId}
          {projectId}
          loading={interpLoading}
          jobId={interpJobId}
          pending={interpPending}
          onback={() => (step = distributorStepMode !== 'none' ? 'distributor-map' : 'map')}
          onnext={() => (step = 'details')}
          oncancel={handleClose}
        />
      {:else}
        <BOMImportStepDetails
          bind:label
          bind:notes
          {canImport}
          {importing}
          hasDistributorStep={distributorStepMode !== 'none'}
          onback={() => (step = 'interpret')}
          onimport={handleFinalImport}
          oncancel={handleClose}
        />
      {/if}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
