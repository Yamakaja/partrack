// Snapshot of the main parts list's current search, published so the floating
// list panel's "From the parts search" source verb can act on the same filtered
// set the table shows — across all pages, not just the rows loaded. Only the
// URL-synced PartsTable (the real Parts page, not embedded pickers) populates
// it, and it clears the flag when that table unmounts.

export interface PartsSearchState {
  /** A parts-search view is currently mounted. */
  active: boolean;
  /** Serialized API filter string (the output of parts.ts `partsSearchQuery`). */
  query: string;
  /** Current total result count across all pages. */
  total: number;
}

export const partsSearchStore = $state<PartsSearchState>({
  active: false,
  query: '',
  total: 0,
});
