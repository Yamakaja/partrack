# DataTable

Generic table component at `web/src/lib/components/table/`.

```ts
import { DataTable, LocalDataSource } from '$lib/components/table/index.js';
import type { ColumnSpec } from '$lib/components/table/index.js';
```

---

## Quick start

```svelte
<script lang="ts">
  type Row = { id: string; name: string; amount: number; created_at: string };

  const columns: ColumnSpec<Row>[] = [
    { key: 'name',       header: 'Name',    type: 'text' },
    { key: 'amount',     header: 'Amount',  type: 'currency', align: 'right' },
    { key: 'created_at', header: 'Created', type: 'date' },
  ];

  const source = new LocalDataSource<Row>(() => rows, columns);
</script>

<DataTable {source} getRowKey={(r) => r.id} />
```

---

## Column types

| `type`     | Default alignment | Notes |
|---|---|---|
| `text`     | left  | substring filter |
| `numeric`  | right | `unit` suffix rendered, float sort |
| `currency` | right | toFixed(2) display, toFixed(4) in CSV |
| `enum`     | left  | checkbox filter dropdown |
| `date`     | left  | date-range filter popover |
| `custom`   | left  | requires `render` or `renderers` |

---

## ColumnSpec fields

```ts
{
  key: string;                          // dot-notation supported for nested: "meta.sku"
  header: string;
  type: 'text' | 'numeric' | 'currency' | 'enum' | 'date' | 'custom';
  sortable?: boolean;                   // default true
  filterable?: boolean;                 // default false
  filterValues?: string[];              // static enum options; omit to auto-derive from data
  filterLabels?: Record<string, string>; // display labels for raw filter values
  unit?: string;                        // suffix for numeric columns, e.g. "Ω"
  align?: 'left' | 'right';
  width?: string;                       // CSS value, e.g. "120px"
  hidden?: boolean;                     // exclude from render and export
  exportOnly?: boolean;                 // CSV only, not rendered in table
  exportValue?: (row: T) => string;     // custom CSV formatter
}
```

---

## LocalDataSource

Client-side sort, filter, and export over an in-memory dataset.

```ts
const source = new LocalDataSource<Row>(
  () => query.data?.items ?? [],  // reactive getter — re-evaluated on every access
  columns,
);
```

- Sorting: click a header to cycle asc → desc → off.
- Filtering: `source.setFilter(key, new Set(['value']))` / `source.clearFilters()`.
- Export: `source.getExportRows()` returns the current filtered+sorted rows.
- `getFilterValues(key)`: returns distinct sorted values from the live data (used automatically when `filterable: true` and no static `filterValues`).

---

## DataTable props

```ts
source: DataSource<T>
getRowKey: (row: T) => string           // required; must be unique per row

onRowClick?: (row: T) => void           // makes rows cursor-pointer
rowClass?: (row: T) => string           // conditional row classes
renderers?: Record<string, Snippet<[value: unknown, row: T]>>

detailPanel?: Snippet<[row: T]>         // expandable panel below each row
detailPanelOpen?: (row: T) => boolean   // which rows start expanded

actions?: Snippet<[row: T]>             // last column, right-aligned

toolbarLeft?: Snippet                   // e.g. search input
toolbarRight?: Snippet                  // e.g. "Add" button

compact?: boolean                       // reduced row padding
stickyHeader?: boolean                  // default true
emptyMessage?: string
exportFilename?: string                 // default "export.csv"
```

---

## Custom cell rendering

Define snippets in the template; pass via `renderers`:

```svelte
{#snippet renderStatus(value: unknown, row: Row)}
  <Badge variant={statusVariant(value as Status)}>{statusLabel(value as Status)}</Badge>
{/snippet}

<DataTable {source} {getRowKey} renderers={{ status: renderStatus }} />
```

`renderers` takes priority over `col.render`.

---

## Filterable columns

**Enum filter** (checkbox dropdown):
```ts
{ key: 'status', type: 'enum', filterable: true,
  filterValues: ['draft', 'placed', 'received'],
  filterLabels: { draft: 'Draft', placed: 'Placed', received: 'Received' } }
```
Omit `filterValues` to auto-derive distinct values from the loaded data.

**Date range filter** (popover with from/to inputs):
```ts
{ key: 'created_at', type: 'date', filterable: true }
```
Stored internally as `"YYYY-MM-DD|YYYY-MM-DD"` in the filters Map.

---

## Toolbar slots

```svelte
<DataTable {source} {getRowKey}>
  {#snippet toolbarLeft()}
    <SearchInput bind:value={q} />
  {/snippet}
  {#snippet toolbarRight()}
    <Button onclick={openCreateDialog}>Add</Button>
  {/snippet}
</DataTable>
```

Active filter chips and the CSV export button appear automatically in the toolbar when filters are set or `source.exportEnabled` is true.

---

## Detail panels

```svelte
{#snippet detail(row: Row)}
  <div class="p-4">
    <p>{row.notes}</p>
  </div>
{/snippet}

<DataTable {source} {getRowKey} detailPanel={detail} />
```

Each row gets a chevron toggle. Expand state is tracked locally in the component.

---

## Actions column

```svelte
{#snippet actions(row: Row)}
  <Button variant="ghost" size="icon" onclick={() => deleteRow(row)}>
    <Trash2 class="h-4 w-4" />
  </Button>
{/snippet}

<DataTable {source} {getRowKey} {actions} />
```

---

## RemoteDataSource (planned)

`RemoteDataSource` (not yet implemented) will wrap a TanStack Query fetch function and forward sort/filter/pagination params to the backend. Pagination controls render automatically when `source.hasPagination` is true.
