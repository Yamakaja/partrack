# DataTable Component Reference

The DataTable system is a fully-featured, flexible data grid abstraction built with Svelte 5, bits-ui, and Tailwind CSS. It supports two data sources (client-side and server-side), advanced filtering, sorting, pagination, CSV export, column visibility toggling, expandable detail panels, and custom cell renderers.

## File Structure

```
web/src/lib/components/table/
├── types.ts                      # TypeScript interfaces for the entire system
├── index.ts                      # Public exports
├── DataTable.svelte              # Top-level orchestrator component
├── DataTableHeader.svelte        # Header row: sort buttons + filter popovers
├── DataTableBody.svelte          # Data rows: loading skeletons, empty state, detail panels
├── DataTableToolbar.svelte       # Top toolbar: title, active filter badges, column picker
├── DataTablePagination.svelte    # Bottom pagination controls
├── DataTableExport.svelte        # CSV export button
├── LocalDataSource.svelte.ts     # Client-side data source implementation
├── RemoteDataSource.svelte.ts    # Server-side data source implementation
├── DateRangeFilterPopover.svelte # Date range picker filter UI
├── NumericFilterPopover.svelte   # Numeric/currency range filter UI
└── ListFilterPopover.svelte      # Enum/text multi-select filter UI
```

---

## ColumnSpec\<T\>

Defines a single column. Pass an array of these to both `LocalDataSource` and `RemoteDataSource`.

```typescript
interface ColumnSpec<T> {
  key: string;                           // Field accessor; supports dot-notation ("user.name")
  header: string;                        // Column header label
  type: 'text' | 'numeric' | 'currency' | 'enum' | 'date' | 'custom';
  sortable?: boolean;                    // Default true; false hides sort button entirely
  filterable?: boolean;                  // Enables filter popover in header
  filterValues?: string[];              // Predefined filter options (required for enum type)
  filterLabels?: Record<string, string>; // Human-readable labels for filter values
  unit?: string;                         // Shown in numeric/currency cells and filter UI
  align?: 'left' | 'right';             // Default: 'left' for text, 'right' for numeric/currency
  width?: string;                        // CSS width value (e.g. "120px")
  hidden?: boolean;                      // Hidden from display but included in CSV export
  exportOnly?: boolean;                  // Hidden from display, not filterable, shown in export
  render?: Snippet<[value: unknown, row: T]>;    // Custom cell renderer
  exportValue?: (row: T) => string;      // Custom CSV export formatter
}
```

### Type Behaviors

| Type | Default Alignment | Sort | Filter UI | Filter Encoding |
|------|-------------------|------|-----------|-----------------|
| `text` | left | `localeCompare` | ListFilterPopover (substring) | `Set<string>` (any value matches) |
| `numeric` | right | numeric | NumericFilterPopover | `">=N"`, `"<=N"`, `"=N"`, or `"N\|N"` (range) |
| `currency` | right | numeric | NumericFilterPopover | same as numeric |
| `enum` | left | `localeCompare` | ListFilterPopover (exact match) | `Set<string>` |
| `date` | left | chronological | DateRangeFilterPopover | `"YYYY-MM-DD\|YYYY-MM-DD"` |
| `custom` | left | none | none | — |

---

## DataTableProps\<T\>

Props accepted by `<DataTable>`.

```typescript
interface DataTableProps<T> {
  // Required
  source: DataSource<T>;                                           // LocalDataSource or RemoteDataSource
  getRowKey: (row: T) => string;                                  // Unique stable key per row

  // Row interaction
  onRowClick?: (row: T) => void;                                  // Click handler (adds cursor-pointer)
  rowClass?: (row: T) => string;                                  // Dynamic Tailwind classes per row

  // Custom cell renderers (highest priority; overrides ColumnSpec.render)
  renderers?: Record<string, Snippet<[value: unknown, row: T]>>;

  // Detail panel (expandable row below each data row)
  detailPanel?: Snippet<[row: T]>;
  detailPanelOpen?: (row: T) => boolean;                          // Initial open state

  // Actions column (right-aligned, no header)
  actions?: Snippet<[row: T]>;

  // Toolbar
  title?: string;                                                 // Displayed uppercase in toolbar
  toolbarLeft?: Snippet;                                          // Custom content, left of filter chips
  toolbarRight?: Snippet;                                         // Custom content, right side

  // Display options
  compact?: boolean;                                              // Reduced cell padding (py-2 vs py-4)
  stickyHeader?: boolean;                                         // Sticky header row with z-10
  showColumnPicker?: boolean;                                     // Column visibility dropdown in toolbar
  emptyMessage?: string;                                          // Text shown when no rows
  emptyIcon?: Component;                                          // Icon for empty state

  // Export
  exportFilename?: string;                                        // Default "export.csv"
}
```

---

## LocalDataSource

All filtering, sorting, and pagination happen in-memory. No pagination UI is shown.

### Constructor

```typescript
new LocalDataSource<T>(
  getData: () => T[],                           // Reactive getter (called in $derived)
  columns: ColumnSpec<T>[],
  getValue?: (row: T, key: string) => unknown,  // Optional custom field accessor
)
```

### Key Properties

| Property | Value | Notes |
|----------|-------|-------|
| `rows` | filtered + sorted array | Reactive |
| `totalRows` | `rows.length` | |
| `loading` | always `false` | |
| `hasPagination` | `false` | Pagination component hidden |
| `page` | `1` | Fixed |
| `pageSize` | `50` | Fixed |
| `exportEnabled` | `true` | |

### Methods

```typescript
setSort(key: string, direction: 'asc' | 'desc'): void
setFilter(key: string, values: Set<string>): void
clearFilters(): void
setColumnHidden(key: string, hidden: boolean): void
getFilterValues(key: string): string[]   // Returns distinct values from current data
async getExportRows(): Promise<T[]>      // Returns all filtered rows
refresh(): void                          // No-op
```

### Filter Logic (in-memory)

- **text**: Case-insensitive substring match; row matches if cell value contains _any_ selected value
- **numeric/currency**: Decodes operator strings (`>=N`, `<=N`, `=N`, `N|N`) and applies arithmetic comparison
- **enum**: Exact match; row matches if cell value is in the selected Set
- **date**: Decodes `"YYYY-MM-DD|YYYY-MM-DD"`; compares timestamps (end date extended to 23:59:59)

### Example

```typescript
const columns: ColumnSpec<Order>[] = [
  { key: 'id', header: 'ID', type: 'text', sortable: false, width: '120px' },
  {
    key: 'status',
    header: 'Status',
    type: 'enum',
    filterable: true,
    filterValues: ['draft', 'received'],
    filterLabels: { draft: 'Draft', received: 'Received' },
  },
  { key: 'created_at', header: 'Created', type: 'date', filterable: true },
];

const source = new LocalDataSource<Order>(
  () => ordersQuery.data?.items ?? [],
  columns,
);
```

---

## RemoteDataSource

Delegates filtering, sorting, and pagination to a server-side fetch function. Always shows pagination UI.

### Constructor

```typescript
new RemoteDataSource<T>(
  columns: ColumnSpec<T>[],
  fetchFn: (params: RemoteFetchParams) => Promise<{ items: T[]; total: number }>,
  getExtraParams?: () => Record<string, unknown>,   // Context merged into params.extra; triggers refetch on change
  options?: { pageSize?: number },                  // Default pageSize: 50
)
```

### RemoteFetchParams

```typescript
interface RemoteFetchParams {
  page: number;                         // 1-indexed
  pageSize: number;
  sortKey: string | null;               // Active column key, or null
  sortDirection: 'asc' | 'desc';
  filters: Map<string, Set<string>>;   // Raw encoded filter values (same encoding as LocalDataSource)
  extra: Record<string, unknown>;      // From getExtraParams()
}
```

### Key Properties

| Property | Value | Notes |
|----------|-------|-------|
| `rows` | current page items | |
| `totalRows` | server total | |
| `loading` | `true` during fetch | |
| `hasPagination` | `true` | Always shown |
| `page` | 1-indexed | |
| `pageSize` | configurable | |
| `exportEnabled` | `true` | |

### Fetch Behavior

- Auto-fetches on construction via `$effect`
- Refetches on any change to: `page`, `pageSize`, `sortKey`, `sortDirection`, `filters`, or `extra`
- **100ms debounce** on refetch
- Resets to page 1 on sort or filter change
- On fetch error: leaves existing rows visible (no error UI)
- `getFilterValues()` returns only predefined `column.filterValues` (no dynamic server lookup)

### Export Behavior

`getExportRows()` fetches up to **1000 rows** in batches of 100, applying current filters/sort but ignoring pagination. Stops early if a batch returns fewer than 100 rows.

### Example

```typescript
const source = new RemoteDataSource<Part>(
  columns,
  async (params) => {
    return listParts({
      team_id: teamId,
      q: (params.extra.q as string) || undefined,
      stock_status: [...(params.filters.get('stock_status') ?? [])],
      limit: params.pageSize,
      offset: (params.page - 1) * params.pageSize,
      sort_key: params.sortKey ?? undefined,
      sort_dir: params.sortDirection,
    });
  },
  () => ({ q: searchQuery }),    // Changing searchQuery triggers refetch
  { pageSize: 50 },
);
```

---

## Filter Popovers

These are used internally by `DataTableHeader`. You rarely need to use them directly.

### DateRangeFilterPopover

Triggered for columns with `type: 'date'` and `filterable: true`. Uses bits-ui `DateRangePicker`. Filter encoded as `"YYYY-MM-DD|YYYY-MM-DD"`.

### NumericFilterPopover

Triggered for `type: 'numeric'` or `'currency'` with `filterable: true`. Four modes selectable by the user:

| Mode | UI Label | Encoding |
|------|----------|----------|
| Equals | `=` | `"=N"` |
| At least | `≥` | `">=N"` |
| At most | `≤` | `"<=N"` |
| Range | `↔` | `"N\|N"` |

Shows `unit` label if provided on the column.

### ListFilterPopover

Triggered for `type: 'enum'` columns or any column with `filterValues` and `filterable: true`. Also used for `type: 'text'` if `filterValues` are provided (exact substring options).

Features:
- Search box filters visible options
- **Drag-select**: hold mouse button down and drag over items to bulk-toggle
- Selected count shown in footer
- Calls `source.getFilterValues(key)` for dynamic values from data (LocalDataSource only)

---

## Component Hierarchy

```
DataTable.svelte
├── DataTableToolbar.svelte
│   ├── title (uppercase, muted)
│   ├── toolbarLeft snippet
│   ├── Active filter chips (one per active filter key)
│   │   └── "Clear all" button (if >1 filter active)
│   ├── toolbarRight snippet
│   ├── DataTableExport (if exportEnabled)
│   └── Column picker dropdown (if showColumnPicker)
├── <table> (bits-ui Table.Root)
│   ├── DataTableHeader.svelte
│   │   └── Per-column <th>:
│   │       ├── Sort button (if sortable !== false)
│   │       └── Filter popover (if filterable):
│   │           ├── DateRangeFilterPopover   (type: 'date')
│   │           ├── NumericFilterPopover     (type: 'numeric'|'currency')
│   │           └── ListFilterPopover        (type: 'enum' or has filterValues)
│   └── DataTableBody.svelte
│       ├── Skeleton rows (if loading, 5 rows)
│       ├── Empty state row (if !loading && rows.length === 0)
│       └── Per-row:
│           ├── Expand toggle (if detailPanel provided)
│           ├── Data cells (renderer priority: renderers prop > ColumnSpec.render > defaultCellValue)
│           ├── Actions cell (if actions provided)
│           └── Detail panel row (if expanded, spans all columns)
└── DataTablePagination.svelte (if hasPagination)
    ├── "X of Y rows" label
    ├── Rows-per-page selector
    └── Prev/Next page buttons
```

---

## Usage Patterns

### Pattern 1: LocalDataSource (simple list)

```svelte
<script lang="ts">
  import { DataTable, LocalDataSource, type ColumnSpec } from '$lib/components/table';

  const columns: ColumnSpec<Order>[] = [
    { key: 'id', header: 'ID', type: 'text', width: '120px' },
    {
      key: 'status',
      header: 'Status',
      type: 'enum',
      filterable: true,
      filterValues: ['draft', 'pending', 'received'],
      filterLabels: { draft: 'Draft', pending: 'Pending', received: 'Received' },
    },
    { key: 'created_at', header: 'Created', type: 'date', filterable: true },
    { key: 'total', header: 'Total', type: 'currency', filterable: true, unit: 'USD' },
  ];

  const source = new LocalDataSource<Order>(
    () => ordersQuery.data?.items ?? [],
    columns,
  );
</script>

{#snippet renderStatus(value: unknown, _row: Order)}
  <Badge variant={statusVariant(value as string)}>{value}</Badge>
{/snippet}

<DataTable
  {source}
  getRowKey={(o) => o.id}
  onRowClick={(o) => goto(`/orders/${o.id}`)}
  renderers={{ status: renderStatus }}
  emptyMessage="No orders yet"
  exportFilename="orders.csv"
/>
```

### Pattern 2: RemoteDataSource with extra params

```svelte
<script lang="ts">
  import { DataTable, RemoteDataSource, type ColumnSpec } from '$lib/components/table';

  let searchQuery = $state('');

  const source = new RemoteDataSource<Part>(
    columns,
    async (params) => {
      return listParts({
        team_id: teamId,
        q: (params.extra.q as string) || undefined,
        limit: params.pageSize,
        offset: (params.page - 1) * params.pageSize,
      });
    },
    () => ({ q: searchQuery }),    // Changing searchQuery triggers refetch
  );
</script>

<DataTable
  {source}
  getRowKey={(p) => p.id}
  showColumnPicker
  exportFilename="parts.csv"
>
  {#snippet toolbarLeft()}
    <input bind:value={searchQuery} placeholder="Search..." />
  {/snippet}
</DataTable>
```

### Pattern 3: Custom renderers

Snippets receive `(value: unknown, row: T)`. The `value` is the raw field value; `row` gives access to the full object.

```svelte
{#snippet renderQty(_value: unknown, row: Part)}
  <span class="tabular-nums font-mono">
    {row.inventory_entry_id != null ? (row.quantity_on_hand ?? 0) : '—'}
  </span>
{/snippet}

{#snippet renderLifecycle(value: unknown, _row: Part)}
  {@const status = value as string}
  <Badge variant={status === 'active' ? 'default' : 'secondary'}>
    {status}
  </Badge>
{/snippet}

<DataTable
  {source}
  getRowKey={(p) => p.id}
  renderers={{
    quantity_on_hand: renderQty,
    lifecycle_status: renderLifecycle,
  }}
/>
```

### Pattern 4: Detail panel

```svelte
{#snippet detailContent(row: Part)}
  <div class="p-4 bg-accent/30 space-y-2">
    <p class="text-sm">{row.description}</p>
    <ParameterTable parameters={row.parameters} />
  </div>
{/snippet}

<DataTable
  {source}
  getRowKey={(p) => p.id}
  detailPanel={detailContent}
  detailPanelOpen={(row) => row.id === initiallyExpandedId}
/>
```

### Pattern 5: Row actions

```svelte
{#snippet rowActions(row: Part)}
  <div class="flex gap-2">
    <Button size="sm" onclick={() => openEditDialog(row)}>Edit</Button>
    <Button size="sm" variant="destructive" onclick={() => confirmDelete(row)}>Delete</Button>
  </div>
{/snippet}

<DataTable
  {source}
  getRowKey={(p) => p.id}
  actions={rowActions}
/>
```

---

## Key Implementation Details

### Nested Field Access

All column `key` values support dot-notation for nested objects:

```typescript
{ key: 'user.profile.name', ... }   // Accesses row.user.profile.name
```

Implemented via `getNestedValue(obj, key)` exported from `LocalDataSource.svelte.ts`. LocalDataSource calls a custom `getValue` if provided, else falls back to `getNestedValue`. The `DataTableBody` also uses `getNestedValue` for rendering.

### Renderer Priority

For each cell, rendering is evaluated in this order:
1. **`renderers[key]`** prop on `<DataTable>` — always wins
2. **`ColumnSpec.render`** — per-column fallback
3. **`defaultCellValue(col, value)`** — built-in formatting:
   - `currency`: `$X.XX` (or `—` if null)
   - `date`: `toLocaleDateString()` (or `—` if null)
   - everything else: string cast

### Column Visibility

- `hidden: true` in `ColumnSpec`: hidden from display, **visible in CSV export**
- `exportOnly: true` in `ColumnSpec`: hidden from display, not filterable, **visible in CSV export**
- `showColumnPicker` prop + `source.setColumnHidden()`: runtime toggle via toolbar dropdown (persists in source state only, not localStorage)

### CSV Export

- Button appears in toolbar when `source.exportEnabled` is true
- Calls `source.getExportRows()` for all matching rows
- Per-column formatting: `ColumnSpec.exportValue(row)` if provided, else `defaultFormat(col, row)`
- Output includes UTF-8 BOM for Excel compatibility
- Values with commas, quotes, or newlines are double-quoted and escaped

### Filter Chip Labels

Active filters in the toolbar show human-readable chip labels:
- **enum**: uses `filterLabels[value]` if provided, else raw value
- **date**: formats `"YYYY-MM-DD|YYYY-MM-DD"` as `"After YYYY-MM-DD"` / `"Before YYYY-MM-DD"` / `"YYYY-MM-DD – YYYY-MM-DD"`
- **numeric/currency**: formats encoded operators as `"≥ N"` / `"≤ N"` / `"= N"` / `"N – N"`
- **text**: shows raw value

---

## Exports from `index.ts`

```typescript
export { default as DataTable } from './DataTable.svelte';
export { default as DataTableHeader } from './DataTableHeader.svelte';
export { default as DataTableBody } from './DataTableBody.svelte';
export { default as DataTableToolbar } from './DataTableToolbar.svelte';
export { default as DataTablePagination } from './DataTablePagination.svelte';
export { default as DataTableExport } from './DataTableExport.svelte';
export { LocalDataSource, getNestedValue } from './LocalDataSource.svelte.js';
export { RemoteDataSource } from './RemoteDataSource.svelte.js';
export type { RemoteFetchParams } from './RemoteDataSource.svelte.js';
export type { ColumnSpec, DataSource, DataTableProps } from './types.js';
```

---

## Limitations

- **No built-in row selection** or bulk actions — use `onRowClick` or custom `actions` snippet
- **RemoteDataSource export cap**: 1000 rows max (fetched in pages of 100)
- **RemoteDataSource filter values**: `getFilterValues()` returns only `column.filterValues` — no dynamic server lookup
- **No column reordering** — order is fixed by the `columns` array
- **No inline editing** — tables are read-only; use dialogs or separate forms
- **Detail panel state is not persisted** — collapses on component re-render
- **No multi-page filter persistence** — filters reset when component is destroyed
