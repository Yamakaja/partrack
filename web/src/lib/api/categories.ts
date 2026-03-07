import { request } from './client';
import type { Category } from '$lib/types/categories';

export async function listCategories(): Promise<Category[]> {
  const data = await request<{ categories: Category[] }>('GET', '/api/v1/categories');
  return data.categories;
}

export async function listPartCategories(teamId: string): Promise<Set<number>> {
  const data = await request<{ category_ids: number[] }>('GET', `/api/v1/teams/${teamId}/parts/categories`);
  return new Set(data.category_ids);
}
