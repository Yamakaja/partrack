import type { StorageLocation, GridConfig } from '$lib/types/inventory.js';
import type { PartLocation } from './types.js';

export function locationPath(id: string, locations: StorageLocation[]): string {
  const path: string[] = [];
  let cur = locations.find(l => l.id === id);
  while (cur) {
    path.unshift(cur.label);
    cur = cur.parent_id ? locations.find(l => l.id === cur!.parent_id) : undefined;
  }
  return path.join(' → ');
}

export function coordLabel(coord: number[], gridConfig: GridConfig | null): string {
  if (!gridConfig || coord.length === 0) return coord.join(',');
  const levels = gridConfig.levels;
  const display = gridConfig.display ?? 'alphanumeric';
  const parts: string[] = [];
  let offset = 0;
  for (const level of levels) {
    const dims = level.dims.length;
    const slice = coord.slice(offset, offset + dims);
    if (slice.length === 0) break;
    if (dims === 1) {
      parts.push(display === 'alphanumeric' ? String.fromCharCode(65 + slice[0]) : String(slice[0] + 1));
    } else {
      const row = display === 'alphanumeric' ? String.fromCharCode(65 + slice[0]) : String(slice[0] + 1);
      const col = String(slice[1] + 1);
      parts.push(`${row}${col}`);
    }
    offset += dims;
  }
  return parts.join('/');
}

/** Full label including coordinate, e.g. "Cabinet A → Tray B / B3" */
export function fullLocationLabel(loc: PartLocation, locations: StorageLocation[]): string {
  const path = locationPath(loc.id, locations);
  if (!loc.coordinate || loc.coordinate.length === 0) return path;
  const location = locations.find(l => l.id === loc.id);
  const label = coordLabel(loc.coordinate, location?.grid_config ?? null);
  return `${path} / ${label}`;
}
