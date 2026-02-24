export interface Project {
  id: string;
  team_id: string;
  name: string;
  description: string | null;
  active_bom_revision_id: string | null;
  bom_import_config: Record<string, string> | null;
  created_at: string;
  created_by: string;
}

export interface ProjectSummary {
  id: string;
  name: string;
  description: string | null;
  active_bom_revision_id: string | null;
  active_bom_revision_label: string | null;
  created_at: string;
}

export interface Revision {
  id: string;
  label: string;
  imported_at: string;
  notes: string | null;
  imported_by_display_name: string;
  item_count: number;
  resolved_count: number;
  unresolved_count: number;
  is_active: boolean;
}

export interface RevisionDetail {
  id: string;
  label: string;
  imported_at: string;
  notes: string | null;
  imported_by_display_name: string;
  is_active: boolean;
  items: BOMLineItem[];
}

export interface BOMLineItem {
  id: string;
  reference_designators: string;
  quantity: number;
  part_name: string | null;
  raw_description: string | null;
  manufacturer: string | null;
  mpn: string | null;
  sku: string | null;
  distributor_name: string | null;
  has_linked_distributor: boolean;
  resolved_part_id: string | null;
  resolved_part_mpn: string | null;
  substitutes: BOMSubstitute[];
}

export interface BOMSubstitute {
  part_id: string;
  part_mpn: string | null;
}

export interface StockCheckItem {
  bom_line_item_id: string;
  reference_designators: string;
  quantity_per_board: number;
  required_quantity: number;
  quantity_on_hand: number;
  shortfall: number;
  resolved_part_id: string | null;
  resolved_part_mpn: string | null;
  stock_status: 'in_stock' | 'partial' | 'out_of_stock';
}

export interface StockCheckResult {
  revision_id: string;
  build_quantity: number;
  fully_buildable: number;
  items: StockCheckItem[];
}
