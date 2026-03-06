import type { ColumnSpec, DataSource } from './types.js';

export function getNestedValue(obj: unknown, key: string): unknown {
  return key.split('.').reduce((acc: unknown, part) => {
    if (acc !== null && acc !== undefined && typeof acc === 'object') {
      return (acc as Record<string, unknown>)[part];
    }
    return undefined;
  }, obj);
}

function compareValues(
  a: unknown,
  b: unknown,
  type: ColumnSpec<unknown>['type'],
): number {
  if (a === null || a === undefined) return 1;
  if (b === null || b === undefined) return -1;

  switch (type) {
    case 'numeric':
    case 'currency':
      return parseFloat(String(a)) - parseFloat(String(b));
    case 'date':
      return new Date(String(a)).getTime() - new Date(String(b)).getTime();
    default:
      return String(a).localeCompare(String(b));
  }
}

export class LocalDataSource<T> implements DataSource<T> {
  // Private reactive state
  #getData: () => T[];
  #rawColumns: ColumnSpec<T>[] = [];
  #hiddenOverrides = $state<Record<string, boolean>>({});
  #getValue: (row: T, key: string) => unknown = (row, key) => getNestedValue(row, key);

  columns: ColumnSpec<T>[] = $derived(
    this.#rawColumns.map((c) => ({
      ...c,
      hidden: this.#hiddenOverrides[c.key] ?? c.hidden ?? false,
    })),
  );

  sortKey = $state<string | null>(null);
  sortDirection = $state<'asc' | 'desc'>('asc');
  filters = $state(new Map<string, Set<string>>());

  // Derived: filtered + sorted rows
  rows: T[] = $derived.by(() => {
    let data = this.#getData();

    // Apply filters
    for (const [key, values] of this.filters.entries()) {
      if (values.size === 0) continue;
      const col = this.columns.find((c) => c.key === key);
      if (!col) continue;

      data = data.filter((row) => {
        const val = this.#getValue(row, key);
        if (col.type === 'date') {
          // Date range: filter Set holds one string "YYYY-MM-DD|YYYY-MM-DD"
          const encoded = [...values][0] ?? '|';
          const [from, to] = encoded.split('|');
          const ts = new Date(String(val)).getTime();
          if (from && ts < new Date(from).getTime()) return false;
          if (to && ts > new Date(to + 'T23:59:59').getTime()) return false;
          return true;
        }
        if (col.type === 'enum') {
          return values.has(String(val ?? ''));
        }
        // text: case-insensitive substring match against first filter value
        const needle = [...values][0]?.toLowerCase() ?? '';
        return String(val ?? '').toLowerCase().includes(needle);
      });
    }

    // Apply sort
    if (this.sortKey !== null) {
      const key = this.sortKey;
      const col = this.columns.find((c) => c.key === key);
      const type = col?.type ?? 'text';
      const dir = this.sortDirection === 'asc' ? 1 : -1;

      data = [...data].sort((a, b) => {
        const av = this.#getValue(a, key);
        const bv = this.#getValue(b, key);
        return dir * compareValues(av, bv, type);
      });
    }

    return data;
  });

  totalRows: number = $derived(this.rows.length);

  loading = false;
  page = 1;
  pageSize = 50;
  hasPagination = false;
  exportEnabled = true;

  constructor(
    getData: () => T[],
    columns: ColumnSpec<T>[],
    getValue?: (row: T, key: string) => unknown,
  ) {
    this.#getData = getData;
    this.#rawColumns = columns;
    if (getValue) this.#getValue = getValue;
  }

  setColumnHidden(key: string, hidden: boolean): void {
    this.#hiddenOverrides[key] = hidden;
  }

  setPage(_page: number): void {}
  setPageSize(_size: number): void {}

  setSort(key: string, direction: 'asc' | 'desc'): void {
    if (this.sortKey === key && this.sortDirection === direction) {
      // Same key + direction → toggle off
      this.sortKey = null;
      this.sortDirection = 'asc';
    } else {
      this.sortKey = key;
      this.sortDirection = direction;
    }
  }

  setFilter(key: string, values: Set<string>): void {
    if (values.size === 0) {
      this.filters.delete(key);
    } else {
      this.filters.set(key, values);
    }
    // Reassign to trigger reactivity
    this.filters = new Map(this.filters);
  }

  clearFilters(): void {
    this.filters = new Map();
  }

  async getExportRows(): Promise<T[]> {
    return this.rows;
  }

  getFilterValues(key: string): string[] {
    const values = new Set<string>();
    for (const row of this.#getData()) {
      const val = this.#getValue(row, key);
      if (val !== null && val !== undefined && String(val) !== '') {
        values.add(String(val));
      }
    }
    return [...values].sort((a, b) => a.localeCompare(b));
  }

  refresh(): void {
    // No-op; caller updates the data getter
  }
}
