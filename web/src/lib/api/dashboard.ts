import { request } from './client.js';

export interface DashboardStats {
  project_count: number;
  active_project_count: number;
  tracked_part_count: number;
  in_stock_part_count: number;
  total_quantity_on_hand: number;
  cart_item_count: number;
  outstanding_order_count: number;
  inventory_below_restock_count: number;
}

export interface DashboardRecentProject {
  id: string;
  name: string;
  status: string;
  active_revision_label: string | null;
  active_revision_id: string | null;
}

export interface DashboardInventoryAlert {
  part_id: string;
  mpn: string | null;
  description: string | null;
  quantity_on_hand: number;
  min_stock_level: number;
  inventory_entry_id: string;
}

export interface DashboardOrder {
  id: string;
  distributor_name: string;
  created_at: string;
  item_count: number;
  status: string;
}

export interface DashboardActivityItem {
  type: 'build' | 'checkout' | 'order_receipt';
  timestamp: string;
  summary: string;
}

export interface DashboardBuildEntry {
  id: string;
  project_name: string;
  revision_label: string;
  quantity: number;
  created_at: string;
  created_by_name: string | null;
}

export interface DashboardBuildPipeline {
  stages: { draft: DashboardBuildEntry[]; pending: DashboardBuildEntry[]; ordered: DashboardBuildEntry[]; ready: DashboardBuildEntry[] };
  counts: { draft: number; pending: number; ordered: number; ready: number };
}

export interface Dashboard {
  stats: DashboardStats;
  recent_projects: DashboardRecentProject[];
  inventory_alerts: DashboardInventoryAlert[];
  outstanding_orders: DashboardOrder[];
  recent_activity: DashboardActivityItem[];
  build_pipeline: DashboardBuildPipeline;
}

export function getDashboard(teamId: string) {
  return request<Dashboard>('GET', `/api/v1/teams/${teamId}/dashboard`);
}
