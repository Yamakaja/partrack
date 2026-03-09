<script lang="ts">
  import { Button } from '$lib/components/ui/button/index.js';
  import { type ColumnRole, ROLE_LABELS, truncateRow, checkForDuplicates } from './bom-import-utils.js';

  let {
    headerRow,
    previewRows,
    hasHeader,
    columnRoles = $bindable(),
    hasRequiredColumns,
    importing,
    onback,
    onnext,
    oncancel,
  }: {
    headerRow: string[];
    previewRows: string[][];
    hasHeader: boolean;
    columnRoles: ColumnRole[];
    hasRequiredColumns: boolean;
    importing: boolean;
    onback: () => void;
    onnext: () => void;
    oncancel: () => void;
  } = $props();
</script>

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

<!-- Footer -->
<div class="flex shrink-0 items-center justify-between border-t border-border px-6 py-4">
  <Button variant="outline" onclick={onback}>← Back</Button>
  <div class="flex gap-2">
    <Button variant="outline" onclick={oncancel}>Cancel</Button>
    <Button onclick={onnext} disabled={!hasRequiredColumns || importing}>
      Next: Distributor Mapping →
    </Button>
  </div>
</div>
