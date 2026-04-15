<script lang="ts">
  import { untrack } from 'svelte';
  import type { GridConfig } from '$lib/types/inventory.js';
  import GridLevelCells from './GridLevelCells.svelte';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Plus, Trash2 } from 'lucide-svelte';

  interface LevelState {
    name: string;
    is2D: boolean;
    rows: number;
    cols: number;
  }

  interface Props {
    initialValue?: GridConfig | null;
    onChange: (v: GridConfig | null) => void;
  }

  let { initialValue = null, onChange }: Props = $props();

  // Uncontrolled: read initialValue exactly once at mount via untrack().
  // The component is unmounted when the dialog closes, so this is always fresh.
  const iv = untrack(() => initialValue);
  let enabled = $state(iv !== null);
  let display = $state<'alphanumeric' | 'numeric'>(iv?.display ?? 'alphanumeric');
  let levels = $state<LevelState[]>(
    iv?.levels.length
      ? iv.levels.map((l) => ({
          name: l.name ?? '',
          is2D: l.dims.length > 1,
          rows: l.dims[0] ?? 4,
          cols: l.dims[1] ?? 1,
        }))
      : [{ name: '', is2D: false, rows: 4, cols: 1 }],
  );

  const config = $derived.by((): GridConfig | null => {
    if (!enabled) return null;
    return {
      levels: levels.map((l) => {
        const rows = Math.min(Math.max(1, l.rows || 1), 100);
        const cols = Math.min(Math.max(1, l.cols || 1), 100);
        const level: { dims: number[]; name?: string } = {
          dims: l.is2D ? [rows, cols] : [rows],
        };
        if (l.name.trim()) level.name = l.name.trim();
        return level;
      }),
      display,
    };
  });

  $effect(() => {
    onChange(config);
  });

  function addLevel() {
    if (levels.length >= 3) return;
    levels = [...levels, { name: '', is2D: false, rows: 4, cols: 1 }];
  }

  function removeLevel(i: number) {
    if (levels.length <= 1) return;
    levels = levels.filter((_, idx) => idx !== i);
  }

  function updateLevel(i: number, patch: Partial<LevelState>) {
    levels = levels.map((l, idx) => (idx === i ? { ...l, ...patch } : l));
  }

  function parseDim(raw: string): number {
    const v = parseInt(raw, 10);
    return isNaN(v) ? 1 : Math.min(Math.max(1, v), 100);
  }

  const summary = $derived.by((): string => {
    if (!config) return '';
    return config.levels
      .map((l, i) => {
        const total = l.dims.reduce((a, b) => a * b, 1);
        const name = l.name ?? (i === 0 ? 'cell' : 'sub-cell');
        if (i === 0) {
          return l.dims.length === 1
            ? `${total} ${name}${total !== 1 ? 's' : ''}`
            : `${l.dims[0]}×${l.dims[1]} grid (${total} ${name}${total !== 1 ? 's' : ''})`;
        }
        return `${total} ${name}${total !== 1 ? 's' : ''}`;
      })
      .join(', each divided into ');
  });
</script>

<div class="space-y-3">
  <!-- Enable toggle -->
  <label class="flex items-center gap-2 cursor-pointer select-none">
    <input
      type="checkbox"
      checked={enabled}
      onchange={(e: Event) => (enabled = (e.target as HTMLInputElement).checked)}
      class="h-4 w-4 rounded border-input"
    />
    <span class="text-sm font-medium">Enable grid layout</span>
  </label>

  {#if enabled}
    <div class="grid grid-cols-5 gap-4 pt-1">
      <!-- Left: form -->
      <div class="col-span-2 space-y-4">
        <!-- Display format -->
        <div class="space-y-1.5">
          <p class="text-xs font-medium text-muted-foreground uppercase tracking-wide">Cell labels</p>
          <div class="flex gap-4">
            <label class="flex items-center gap-1.5 text-sm cursor-pointer">
              <input type="radio" bind:group={display} value="alphanumeric" class="h-3.5 w-3.5" />
              <span>Alphanumeric <span class="text-muted-foreground text-xs">(A1, B3)</span></span>
            </label>
            <label class="flex items-center gap-1.5 text-sm cursor-pointer">
              <input type="radio" bind:group={display} value="numeric" class="h-3.5 w-3.5" />
              <span>Numeric <span class="text-muted-foreground text-xs">(1, 2…)</span></span>
            </label>
          </div>
        </div>

        <!-- Levels -->
        <div class="space-y-2">
          <p class="text-xs font-medium text-muted-foreground uppercase tracking-wide">
            {levels.length > 1 ? `Levels (${levels.length})` : 'Grid'}
          </p>
          {#each levels as level, i (i)}
            <div class="rounded-md border border-border p-3 space-y-2.5">
              <div class="flex items-center justify-between">
                <span class="text-xs font-medium text-muted-foreground">
                  {levels.length > 1 ? `Level ${i + 1}` : 'Grid'}
                </span>
                {#if levels.length > 1}
                  <button
                    type="button"
                    onclick={() => removeLevel(i)}
                    class="text-muted-foreground hover:text-destructive transition-colors"
                    title="Remove level"
                  >
                    <Trash2 class="h-3.5 w-3.5" />
                  </button>
                {/if}
              </div>

              <!-- Cell name -->
              <div class="space-y-1">
                <Label class="text-xs">
                  Cell name <span class="text-muted-foreground font-normal">(optional)</span>
                </Label>
                <Input
                  class="h-7 text-sm"
                  placeholder={i === 0 ? 'e.g. Slot, Box, Drawer' : 'e.g. Section, Compartment'}
                  value={level.name}
                  oninput={(e: Event) => updateLevel(i, { name: (e.target as HTMLInputElement).value })}
                />
              </div>

              <!-- 1D / 2D segmented control -->
              <div class="flex rounded-md overflow-hidden border border-input text-xs">
                <button
                  type="button"
                  class="flex-1 px-2 py-1 transition-colors {!level.is2D
                    ? 'bg-primary text-primary-foreground'
                    : 'hover:bg-muted text-muted-foreground'}"
                  onclick={() => updateLevel(i, { is2D: false })}
                >
                  1D strip
                </button>
                <button
                  type="button"
                  class="flex-1 px-2 py-1 border-l border-input transition-colors {level.is2D
                    ? 'bg-primary text-primary-foreground'
                    : 'hover:bg-muted text-muted-foreground'}"
                  onclick={() => updateLevel(i, { is2D: true })}
                >
                  2D grid
                </button>
              </div>

              <!-- Dimension inputs -->
              <div class="flex items-end gap-2">
                {#if level.is2D}
                  <div class="flex-1 space-y-1">
                    <Label class="text-xs">Rows</Label>
                    <Input
                      class="h-7 text-sm"
                      type="number"
                      min="1"
                      max="100"
                      value={level.rows}
                      oninput={(e: Event) => updateLevel(i, { rows: parseDim((e.target as HTMLInputElement).value) })}
                    />
                  </div>
                  <span class="text-muted-foreground text-sm pb-1.5">×</span>
                  <div class="flex-1 space-y-1">
                    <Label class="text-xs">Cols</Label>
                    <Input
                      class="h-7 text-sm"
                      type="number"
                      min="1"
                      max="100"
                      value={level.cols}
                      oninput={(e: Event) => updateLevel(i, { cols: parseDim((e.target as HTMLInputElement).value) })}
                    />
                  </div>
                {:else}
                  <div class="flex-1 space-y-1">
                    <Label class="text-xs">Count</Label>
                    <Input
                      class="h-7 text-sm"
                      type="number"
                      min="1"
                      max="100"
                      value={level.rows}
                      oninput={(e: Event) => updateLevel(i, { rows: parseDim((e.target as HTMLInputElement).value) })}
                    />
                  </div>
                {/if}
              </div>
            </div>
          {/each}

          {#if levels.length < 3}
            <button
              type="button"
              onclick={addLevel}
              class="flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
            >
              <Plus class="h-3.5 w-3.5" /> Add level
            </button>
          {/if}
        </div>

        {#if summary}
          <p class="text-xs text-muted-foreground italic">{summary}</p>
        {/if}
      </div>

      <!-- Right: live preview -->
      <div class="col-span-3 space-y-2">
        <p class="text-xs font-medium text-muted-foreground uppercase tracking-wide">Preview</p>
        {#if config}
          <div class="space-y-3">
            {#each config.levels as level, i}
              <div class="rounded-md border border-border p-3 select-none">
                {#if config.levels.length > 1 || level.name}
                  <p class="text-xs text-muted-foreground font-medium mb-2">
                    {level.name || `Level ${i + 1}`}
                  </p>
                {/if}
                <GridLevelCells
                  {level}
                  display={config.display ?? 'alphanumeric'}
                  readonly={true}
                />
              </div>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>
