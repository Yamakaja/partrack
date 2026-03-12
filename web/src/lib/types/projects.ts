export interface Project {
  id: string;
  name: string;
  description: string | null;
  status: 'active' | 'archived' | 'completed';
  active_bom_revision_id: string | null;
  active_bom_revision_label: string | null;
  bom_import_config: Record<string, string> | null;
  preferred_distributor_id: string | null;
  source_url: string | null;
  project_manager_id: string | null;
  project_manager_display_name: string | null;
  created_by_display_name: string;
  created_at: string;
  active_revision_summary: {
    item_count: number;
    resolved_count: number;
    unresolved_count: number;
    ambiguous_count: number;
  } | null;
}

export interface ProjectSummary {
  id: string;
  name: string;
  description: string | null;
  status: 'active' | 'archived' | 'completed';
  active_bom_revision_id: string | null;
  active_bom_revision_label: string | null;
  preferred_distributor_id: string | null;
  source_url: string | null;
  project_manager_id: string | null;
  project_manager_display_name: string | null;
  created_by_display_name: string;
  created_at: string;
}

export function projectStatusVariant(status: string): 'success' | 'outline' | 'secondary' {
  if (status === 'active' || status === 'completed') return 'success';
  return 'secondary';
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
  distributor_name: string;
  has_linked_distributor: boolean;
  resolved_part_id: string | null;
  resolved_part_mpn: string | null;
  resolution_state: string | null;
  ignored: boolean;
  substitutes: BOMSubstitute[];
}

export interface BOMSubstitute {
  part_id: string;
  part_mpn: string | null;
  priority: number;
}

export interface StockCheckItem {
  bom_line_item_id: string;
  reference_designators: string;
  quantity_per_board: number;
  required_quantity: number;
  quantity_on_hand: number;
  quantity_reserved: number;
  quantity_free: number;
  quantity_on_order: number;
  shortfall: number;
  resolved_part_id: string | null;
  resolved_part_mpn: string | null;
  stock_status: 'in_stock' | 'partial' | 'out_of_stock';
  min_stock_qty: number | null;
  below_restock: boolean;
  selected_substitute_id: string | null;
  selected_substitute_mpn: string | null;
  substitute_qty_on_hand: number | null;
}

export interface SubstituteSelection {
  bom_line_item_id: string;
  selected_part_id: string;
  selected_part_mpn: string | null;
}

export interface StockCheckResult {
  revision_id: string;
  build_quantity: number;
  fully_buildable: number;
  items: StockCheckItem[];
}

