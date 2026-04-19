import { request } from './client';
import type { Category } from '$lib/types/categories';

export interface CategoryCount {
  id: number;
  count: number;
}

export async function listCategories(): Promise<Category[]> {
  const data = await request<{ categories: Category[] }>('GET', '/api/v1/categories');
  return data.categories;
}

export async function listPartCategories(teamId: string): Promise<CategoryCount[]> {
  const data = await request<{ categories: CategoryCount[] }>('GET', `/api/v1/teams/${teamId}/parts/categories`);
  return data.categories;
}
