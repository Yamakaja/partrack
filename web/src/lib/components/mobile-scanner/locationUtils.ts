import type { StorageLocation, GridConfig } from '$lib/types/inventory.js';
import type { PartLocation } from './types.js';

/**
 * Decompose a flat coordinate array into parent-level address and last-level cursor,
 * based on the grid config's level definitions.
 *
 * Example: flat=[1,3,0,2], levels=[{dims:[3]},{dims:[4]},{dims:[5,5]}]
 * → { address: [[1],[3]], cursor: [0,2] }
 */
export function decomposeCoordinate(
  flat: number[],
  config: GridConfig
): { address: number[][], cursor: number[] } {
  const levels = config.levels;
  if (levels.length === 0) return { address: [], cursor: flat };

  const address: number[][] = [];
  let offset = 0;

  for (let i = 0; i < levels.length - 1; i++) {
    const d = levels[i].dims.length;
    address.push(flat.slice(offset, offset + d));
    offset += d;
  }

  return { address, cursor: flat.slice(offset) };
}

/**
 * Reassemble parent-level address + last-level cursor into a flat coordinate array.
 * Inverse of decomposeCoordinate.
 */
export function assembleCoordinate(address: number[][], cursor: number[]): number[] {
  return [...address.flatMap(a => a), ...cursor];
}

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
