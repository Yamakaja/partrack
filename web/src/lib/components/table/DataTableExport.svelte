<script lang="ts" generics="T">
  import { Download } from 'lucide-svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import type { DataSource, ColumnSpec } from './types.js';

  let {
    source,
    filename = 'export.csv',
  }: {
    source: DataSource<T>;
    filename?: string;
  } = $props();

  let exporting = $state(false);

  function getNestedValue(obj: T, key: string): unknown {
    return key.split('.').reduce((acc: unknown, part) => {
      if (acc !== null && acc !== undefined && typeof acc === 'object') {
        return (acc as Record<string, unknown>)[part];
      }
      return undefined;
    }, obj as unknown);
  }

  function getCellValue(row: T, key: string): unknown {
    if (source.getValue) return source.getValue(row, key);
    return getNestedValue(row, key);
  }

  function defaultFormat(col: ColumnSpec<T>, row: T): string {
    const val = getCellValue(row, col.key);
    if (val === null || val === undefined) return '';
    switch (col.type) {
      case 'currency':
        return parseFloat(String(val)).toFixed(4);
      case 'date':
        return new Date(String(val)).toISOString();
      default:
        return String(val);
    }
  }

  function csvQuote(value: string): string {
    if (value.includes(',') || value.includes('\n') || value.includes('"')) {
      return `"${value.replace(/"/g, '""')}"`;
    }
    return value;
  }

  async function doExport(): Promise<void> {
    exporting = true;
    try {
      const rows = await source.getExportRows();
      const exportCols = source.columns.filter((c) => !c.hidden);

      const header = exportCols.map((c) => csvQuote(c.header)).join(',');
      const dataRows = rows.map((row) =>
        exportCols
          .map((col) => csvQuote(col.exportValue?.(row) ?? defaultFormat(col, row)))
          .join(','),
      );

      // UTF-8 BOM for Excel compatibility
      const bom = '\uFEFF';
      const csv = bom + [header, ...dataRows].join('\n');

      const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = filename;
      a.click();
      URL.revokeObjectURL(url);
    } finally {
      exporting = false;
    }
  }
</script>

<Button
  variant="outline"
  size="icon"
  onclick={doExport}
  disabled={exporting}
  aria-label="Export CSV"
  title="Export to CSV"
  class="h-8 w-8"
>
  <Download class="h-4 w-4" />
</Button>
