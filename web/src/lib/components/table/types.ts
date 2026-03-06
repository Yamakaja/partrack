import type { Component, Snippet } from 'svelte';

export interface ColumnSpec<T> {
  key: string;
  header: string;
  type: 'text' | 'numeric' | 'currency' | 'enum' | 'date' | 'custom';
  sortable?: boolean;
  filterable?: boolean;
  filterValues?: string[];
  filterLabels?: Record<string, string>;
  unit?: string;
  align?: 'left' | 'right';
  width?: string;
  hidden?: boolean;
  exportOnly?: boolean;
  render?: Snippet<[value: unknown, row: T]>;
  exportValue?: (row: T) => string;
}

export interface DataSource<T> {
  // Column definitions
  columns: ColumnSpec<T>[];

  // Data access
  rows: T[];
  totalRows: number;
  loading: boolean;

  // Pagination
  page: number;
  pageSize: number;
  setPage(page: number): void;
  setPageSize(size: number): void;
  hasPagination: boolean;

  // Sorting
  sortKey: string | null;
  sortDirection: 'asc' | 'desc';
  setSort(key: string, direction: 'asc' | 'desc'): void;

  // Filtering
  filters: Map<string, Set<string>>;
  setFilter(key: string, values: Set<string>): void;
  clearFilters(): void;

  // Export
  exportEnabled: boolean;
  getExportRows(): Promise<T[]>;

  // Column visibility
  setColumnHidden?(key: string, hidden: boolean): void;

  // Lifecycle
  refresh(): void;
  destroy?(): void;

  // Dynamic filter values (distinct values from data)
  getFilterValues?(key: string): string[];
}

export interface DataTableProps<T> {
  source: DataSource<T>;
  onRowClick?: (row: T) => void;
  rowClass?: (row: T) => string;
  getRowKey: (row: T) => string;
  /** Per-column custom renderers, keyed by ColumnSpec.key. Takes priority over col.render. */
  renderers?: Record<string, Snippet<[value: unknown, row: T]>>;
  detailPanel?: Snippet<[row: T]>;
  detailPanelOpen?: (row: T) => boolean;
  actions?: Snippet<[row: T]>;
  toolbarLeft?: Snippet;
  toolbarRight?: Snippet;
  compact?: boolean;
  stickyHeader?: boolean;
  showColumnPicker?: boolean;
  emptyMessage?: string;
  emptyIcon?: Component;
  exportFilename?: string;
}
