import { request } from './client.js';
import type { ParameterDefinition } from '$lib/types/parts.js';

/**
 * The full parameter vocabulary (slug → display name, unit, value type).
 *
 * Reference data seeded by migration 000018 and edited only through the admin
 * screens, so it is safe to cache for a long time. Unlike
 * `getCategoryParameterSchema`, this is not team- or category-scoped — surfaces
 * that render parameters from many categories at once (the revision detail
 * table, for one) would otherwise need a request per category.
 */
export function listParameterDefinitions() {
  return request<ParameterDefinition[]>('GET', '/api/v1/parameter-definitions');
}
