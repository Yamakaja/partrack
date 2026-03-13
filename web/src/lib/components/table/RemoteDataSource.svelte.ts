import type { ColumnSpec, DataSource } from './types.js';

export interface RemoteFetchParams {
  page: number;
  pageSize: number;
  sortKey: string | null;
  sortDirection: 'asc' | 'desc';
  filters: Map<string, Set<string>>;
  extra: Record<string, unknown>;
}

export class RemoteDataSource<T> implements DataSource<T> {
  // Private fields declared first so $derived can reference them
  #fetchFn: (params: RemoteFetchParams) => Promise<{ items: T[]; total: number }>;
  #getExtraParams: (() => Record<string, unknown>) | undefined;
  #rawColumns = $state<ColumnSpec<T>[]>([]);
  #hiddenOverrides = $state<Record<string, boolean>>({});
  #onColumnHiddenCb: ((key: string, hidden: boolean) => void) | undefined;
  #lastExtra: string = '';

  // Derived + reactive public fields
  columns: ColumnSpec<T>[] = $derived(
    this.#rawColumns.map((c) => ({
      ...c,
      hidden: this.#hiddenOverrides[c.key] ?? c.hidden ?? false,
    })),
  );

  sortKey = $state<string | null>(null);
  sortDirection = $state<'asc' | 'desc'>('asc');
  filters = $state(new Map<string, Set<string>>());
  page = $state(1);
  pageSize = $state(50);

  rows = $state<T[]>([]);
  totalRows = $state(0);
  loading = $state(false);
  hasPagination = true;
  exportEnabled = true;

  constructor(
    columns: ColumnSpec<T>[],
    fetchFn: (params: RemoteFetchParams) => Promise<{ items: T[]; total: number }>,
    getExtraParams?: () => Record<string, unknown>,
    options?: { pageSize?: number; onColumnHidden?: (key: string, hidden: boolean) => void },
  ) {
    this.#rawColumns = columns;
    this.#fetchFn = fetchFn;
    this.#getExtraParams = getExtraParams;
    this.#onColumnHiddenCb = options?.onColumnHidden;
    if (options?.pageSize) this.pageSize = options.pageSize;

    $effect(() => {
      const extra = this.#getExtraParams?.() ?? {};

      // When extra params change (e.g. search query), reset to page 1
      const extraKey = JSON.stringify(extra);
      if (extraKey !== this.#lastExtra) {
        this.#lastExtra = extraKey;
        this.page = 1;
      }

      const params: RemoteFetchParams = {
        page: this.page,
        pageSize: this.pageSize,
        sortKey: this.sortKey,
        sortDirection: this.sortDirection,
        filters: this.filters,
        extra,
      };

      const timer = setTimeout(() => {
        this.#doFetch(params);
      }, 100);

      return () => clearTimeout(timer);
    });
  }

  async #doFetch(params: RemoteFetchParams): Promise<void> {
    this.loading = true;
    try {
      const result = await this.#fetchFn(params);
      this.rows = result.items;
      this.totalRows = result.total;
    } catch {
      // Leave existing rows visible on error
    } finally {
      this.loading = false;
    }
  }

  setColumns(columns: ColumnSpec<T>[]): void {
    this.#rawColumns = columns;
  }

  setColumnHidden(key: string, hidden: boolean): void {
    this.#hiddenOverrides[key] = hidden;
    this.#onColumnHiddenCb?.(key, hidden);
  }

  setPage(page: number): void {
    this.page = page;
  }

  setPageSize(size: number): void {
    this.pageSize = size;
    this.page = 1;
  }

  setSort(key: string, direction: 'asc' | 'desc'): void {
    if (this.sortKey === key && this.sortDirection === direction) {
      this.sortKey = null;
      this.sortDirection = 'asc';
    } else {
      this.sortKey = key;
      this.sortDirection = direction;
    }
    this.page = 1;
  }

  setFilter(key: string, values: Set<string>): void {
    if (values.size === 0) {
      this.filters.delete(key);
    } else {
      this.filters.set(key, values);
    }
    this.filters = new Map(this.filters);
    this.page = 1;
  }

  clearFilters(): void {
    this.filters = new Map();
    this.page = 1;
  }

  getFilterValues(key: string): string[] {
    const col = this.#rawColumns.find((c) => c.key === key);
    return col?.filterValues ?? [];
  }

  async getExportRows(): Promise<T[]> {
    const extra = this.#getExtraParams?.() ?? {};
    const all: T[] = [];
    let exportPage = 1;
    const limit = 100;
    const maxRows = 1000;

    while (all.length < maxRows) {
      const result = await this.#fetchFn({
        page: exportPage,
        pageSize: limit,
        sortKey: this.sortKey,
        sortDirection: this.sortDirection,
        filters: this.filters,
        extra,
      });
      all.push(...result.items);
      if (all.length >= result.total || result.items.length < limit) break;
      exportPage++;
    }

    return all;
  }

  refresh(): void {
    this.filters = new Map(this.filters);
  }
}
