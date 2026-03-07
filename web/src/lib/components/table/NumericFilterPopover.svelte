<script lang="ts">
  import { Popover } from 'bits-ui';
  import { Hash } from 'lucide-svelte';
  import { cn } from '$lib/utils/cn.js';
  import { Button } from '$lib/components/ui/button/index.js';

  type Mode = 'eq' | 'gte' | 'lte' | 'range';

  let {
    columnLabel,
    isFiltered,
    unit,
    currentFilter = '',
    onApply,
    onClear,
  }: {
    columnLabel: string;
    isFiltered: boolean;
    unit?: string;
    currentFilter?: string;
    onApply: (encoded: string) => void;
    onClear: () => void;
  } = $props();

  function decode(filter: string): { mode: Mode; a: string; b: string } {
    if (filter.startsWith('>=')) return { mode: 'gte', a: filter.slice(2), b: '' };
    if (filter.startsWith('<=')) return { mode: 'lte', a: filter.slice(2), b: '' };
    if (filter.startsWith('=')) return { mode: 'eq', a: filter.slice(1), b: '' };
    if (filter.includes('|')) {
      const [a, b] = filter.split('|');
      return { mode: 'range', a, b };
    }
    return { mode: 'gte', a: '', b: '' };
  }

  function encode(m: Mode, a: string, b: string): string {
    if (m === 'eq') return `=${a}`;
    if (m === 'gte') return `>=${a}`;
    if (m === 'lte') return `<=${a}`;
    return `${a}|${b}`;
  }

  let open = $state(false);
  let mode = $state<Mode>('gte');
  let valA = $state('');
  let valB = $state('');

  function handleOpenChange(v: boolean) {
    if (v) {
      const decoded = decode(currentFilter);
      mode = decoded.mode;
      valA = decoded.a;
      valB = decoded.b;
    }
    open = v;
  }

  function apply() {
    const hasA = valA.trim() !== '';
    const hasB = valB.trim() !== '';
    if (!hasA && (mode !== 'range' || !hasB)) {
      onClear();
    } else {
      onApply(encode(mode, valA.trim(), valB.trim()));
    }
    open = false;
  }

  function clear() {
    valA = '';
    valB = '';
    onClear();
    open = false;
  }

  const modes: { id: Mode; label: string; title: string }[] = [
    { id: 'eq', label: '=', title: 'Equals' },
    { id: 'gte', label: '≥', title: 'Minimum' },
    { id: 'lte', label: '≤', title: 'Maximum' },
    { id: 'range', label: '↔', title: 'Range' },
  ];
</script>

<Popover.Root {open} onOpenChange={handleOpenChange}>
  <Popover.Trigger>
    <button
      class={cn(
        'rounded p-0.5 hover:bg-accent transition-colors',
        isFiltered ? 'text-primary' : 'text-muted-foreground opacity-60 hover:opacity-100',
      )}
      aria-label="Filter {columnLabel} by value"
    >
      <Hash class="h-3 w-3" />
    </button>
  </Popover.Trigger>

  <Popover.Content
    align="start"
    class="z-50 w-56 rounded-md border border-border bg-popover p-3 text-popover-foreground shadow-md data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
  >
    <p class="text-xs font-medium text-muted-foreground mb-2">Filter {columnLabel}</p>

    <!-- Mode selector -->
    <div class="flex rounded-md border border-border overflow-hidden mb-3">
      {#each modes as m}
        <button
          class={cn(
            'flex-1 py-1 text-sm font-medium transition-colors',
            mode === m.id
              ? 'bg-primary text-primary-foreground'
              : 'bg-background text-muted-foreground hover:bg-accent hover:text-accent-foreground',
          )}
          title={m.title}
          onclick={() => { mode = m.id; valB = ''; }}
        >
          {m.label}
        </button>
      {/each}
    </div>

    <!-- Input(s) -->
    <div class="flex flex-col gap-2 mb-3">
      {#if mode === 'eq'}
        <label class="flex flex-col gap-1">
          <span class="text-xs text-muted-foreground">Value{unit ? ` (${unit})` : ''}</span>
          <input
            type="number"
            bind:value={valA}
            placeholder="0"
            class="h-8 w-full rounded-md border border-input bg-background px-2 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
          />
        </label>
      {:else if mode === 'gte'}
        <label class="flex flex-col gap-1">
          <span class="text-xs text-muted-foreground">Min{unit ? ` (${unit})` : ''}</span>
          <input
            type="number"
            bind:value={valA}
            placeholder="0"
            class="h-8 w-full rounded-md border border-input bg-background px-2 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
          />
        </label>
      {:else if mode === 'lte'}
        <label class="flex flex-col gap-1">
          <span class="text-xs text-muted-foreground">Max{unit ? ` (${unit})` : ''}</span>
          <input
            type="number"
            bind:value={valA}
            placeholder="0"
            class="h-8 w-full rounded-md border border-input bg-background px-2 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
          />
        </label>
      {:else}
        <label class="flex flex-col gap-1">
          <span class="text-xs text-muted-foreground">Min{unit ? ` (${unit})` : ''}</span>
          <input
            type="number"
            bind:value={valA}
            placeholder="0"
            class="h-8 w-full rounded-md border border-input bg-background px-2 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
          />
        </label>
        <label class="flex flex-col gap-1">
          <span class="text-xs text-muted-foreground">Max{unit ? ` (${unit})` : ''}</span>
          <input
            type="number"
            bind:value={valB}
            placeholder="0"
            class="h-8 w-full rounded-md border border-input bg-background px-2 text-xs focus:outline-none focus:ring-1 focus:ring-ring"
          />
        </label>
      {/if}
    </div>

    <div class="flex gap-2">
      <Button size="sm" class="flex-1 h-7 text-xs" onclick={apply}>Apply</Button>
      {#if isFiltered}
        <Button variant="outline" size="sm" class="h-7 text-xs" onclick={clear}>Clear</Button>
      {/if}
    </div>
  </Popover.Content>
</Popover.Root>
