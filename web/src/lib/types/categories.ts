export interface Category {
  id: number;
  slug: string;
  display_name: string;
  parent_id: number | null;
  sort_order: number;
  /** Slug whose magnitude the bare value field encodes; null => not parametric. */
  value_param: string | null;
  primary_params: string[];
}
