<script lang="ts">
  import { Popover } from 'bits-ui';
  import { MapPin } from 'lucide-svelte';
  import type { StorageLocationRef, GridConfig } from '$lib/types/inventory.js';
  import GridLevelCells from './GridLevelCells.svelte';

  interface Props {
    location: StorageLocationRef;
  }

  let { location }: Props = $props();

  let open = $state(false);

  /** Convert a flat coordinate to a human-readable label. */
  function coordinateLabel(gridConfig: GridConfig, coord: number[]): string {
    if (coord.length === 0) return '';
    const display = gridConfig.display ?? 'alphanumeric';
    const parts: string[] = [];
    let offset = 0;
    for (const level of gridConfig.levels) {
      const dimCount = level.dims.length;
      if (offset >= coord.length) break;
      const levelCoords = coord.slice(offset, offset + dimCount);
      offset += dimCount;
      if (level.dims.length === 1) {
        const row = levelCoords[0];
        parts.push(display === 'alphanumeric' ? String.fromCharCode(65 + row) : String(row + 1));
      } else {
        const row = levelCoords[0];
        const col = levelCoords[1] ?? 0;
        parts.push(
          display === 'alphanumeric'
            ? `${String.fromCharCode(65 + row)}${col + 1}`
            : `(${row + 1}, ${col + 1})`,
        );
      }
    }
    return parts.join(' / ');
  }

  /** Full display label: ancestor path + location name + optional coordinate. */
  const label = $derived.by(() => {
    const pathParts: string[] = [];
    if (location.ancestor_path) pathParts.push(location.ancestor_path);
    pathParts.push(location.label);
    let text = pathParts.join(' > ');
    if (location.coordinate && location.coordinate.length > 0 && location.grid_config) {
      const coordLabel = coordinateLabel(location.grid_config, location.coordinate);
      if (coordLabel) text += ` [${coordLabel}]`;
    }
    return text;
  });

  /** Coordinate re-chunked into per-level tuples. */
  const gridAddress = $derived.by((): number[][] => {
    if (!location.coordinate || !location.grid_config) return [];
    const addr: number[][] = [];
    let offset = 0;
    for (const level of location.grid_config.levels) {
      const dimCount = level.dims.length;
      if (offset >= location.coordinate.length) break;
      addr.push(location.coordinate.slice(offset, offset + dimCount));
      offset += dimCount;
    }
    return addr;
  });

  const hasGrid = $derived(!!location.grid_config && !!location.coordinate?.length);

  /** The deepest addressed grid level and its selected cell. */
  const leafLevel = $derived(
    hasGrid && location.grid_config
      ? location.grid_config.levels[gridAddress.length - 1] ?? null
      : null,
  );
  const leafCoord = $derived(gridAddress[gridAddress.length - 1] ?? null);
</script>

<Popover.Root {open} onOpenChange={(v) => { open = v; }}>
  <Popover.Trigger>
    <button
      type="button"
      class="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-sm font-medium
             bg-muted/60 hover:bg-muted border border-border/60 transition-colors
             text-foreground max-w-full"
    >
      <MapPin class="h-3.5 w-3.5 flex-shrink-0 text-muted-foreground" />
      <span class="truncate">{label}</span>
    </button>
  </Popover.Trigger>

  <Popover.Content
    align="start"
    sideOffset={6}
    class="z-50 rounded-md border border-border bg-popover text-popover-foreground shadow-md
           data-[state=open]:animate-in data-[state=closed]:animate-out
           data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
  >
    {#if hasGrid && leafLevel}
      <!-- Grid view with highlighted coordinate -->
      <div class="p-3 space-y-2">
        <p class="text-xs font-medium text-muted-foreground truncate max-w-56">{label}</p>
        <GridLevelCells
          level={leafLevel}
          display={location.grid_config?.display ?? 'alphanumeric'}
          selected={leafCoord}
          readonly
        />
      </div>
    {:else}
      <!-- Simple location name display -->
      <div class="px-3 py-2 text-sm">
        <p class="font-medium">{label}</p>
      </div>
    {/if}
  </Popover.Content>
</Popover.Root>
