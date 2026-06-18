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

/** One parameter in a category's schema, for value-type-aware editing. */
export interface CategoryParamSchema {
  slug: string;
  display_name: string;
  unit_symbol: string;
  value_type: 'numeric' | 'range' | 'text' | 'enum' | 'enum_set';
  /** Catalog-derived value vocabulary (enum/text values, packages in use). */
  options: string[];
}

export interface CategoryParameterSchema {
  category_id: number;
  slug: string;
  value_param: string;
  params: CategoryParamSchema[];
}

/** Per-category parameter schema (labels, value types, enum vocab) for the review UI. */
export function getCategoryParameterSchema(teamId: string, categoryId: number) {
  return request<CategoryParameterSchema>(
    'GET',
    `/api/v1/teams/${teamId}/categories/${categoryId}/parameter-schema`,
  );
}
