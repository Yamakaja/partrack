<script lang="ts" generics="T">
  import { Table } from '$lib/components/ui/table/index.js';
  import DataTableToolbar from './DataTableToolbar.svelte';
  import DataTableHeader from './DataTableHeader.svelte';
  import DataTableBody from './DataTableBody.svelte';
  import DataTablePagination from './DataTablePagination.svelte';
  import type { DataTableProps } from './types.js';

  let {
    source,
    onRowClick,
    rowClass,
    getRowKey,
    renderers,
    headerFilters,
    detailPanel,
    detailPanelOpen,
    actions,
    title,
    toolbarLeft,
    toolbarRight,
    compact = false,
    stickyHeader = true,
    showColumnPicker = false,
    emptyMessage = 'No items found',
    emptyIcon,
    exportFilename = 'export.csv',
  }: DataTableProps<T> = $props();
</script>

<div class="flex flex-col gap-2">
  <DataTableToolbar {source} {title} {toolbarLeft} {toolbarRight} {showColumnPicker} {exportFilename} />

  <Table>
    <DataTableHeader {source} {actions} {detailPanel} {stickyHeader} {headerFilters} />
    <DataTableBody
      {source}
      {getRowKey}
      {onRowClick}
      {rowClass}
      {renderers}
      {detailPanel}
      {detailPanelOpen}
      {actions}
      {compact}
      {emptyMessage}
      {emptyIcon}
    />
  </Table>

  <DataTablePagination {source} />
</div>
