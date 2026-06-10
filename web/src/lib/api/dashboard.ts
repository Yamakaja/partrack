import { request } from './client.js';
import type { Build } from '$lib/types/orders.js';
import type { ActivityEntry } from './activityLog.js';

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
  purchase_order_number: string | null;
  external_reference: string | null;
  created_at: string;
  item_count: number;
  status: string;
}

// Dashboard recent activity items share the unified activity-log shape so the
// dashboard widget and the side panel render through the same component.
export type DashboardActivityItem = ActivityEntry;

export interface Dashboard {
  stats: DashboardStats;
  recent_projects: DashboardRecentProject[];
  inventory_alerts: DashboardInventoryAlert[];
  outstanding_orders: DashboardOrder[];
  recent_activity: DashboardActivityItem[];
  build_pipeline: { draft: Build[]; pending: Build[]; ordered: Build[]; ready: Build[] };
}

export function getDashboard(teamId: string) {
  return request<Dashboard>('GET', `/api/v1/teams/${teamId}/dashboard`);
}
