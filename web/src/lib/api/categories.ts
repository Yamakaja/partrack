import { request } from './client';
import type { Category } from '$lib/types/categories';

export async function listCategories(): Promise<Category[]> {
  const data = await request<{ categories: Category[] }>('GET', '/api/v1/categories');
  return data.categories;
}
