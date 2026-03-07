export interface Category {
  id: number;
  slug: string;
  display_name: string;
  parent_id: number | null;
  sort_order: number;
  primary_params: string[];
}
