<script lang="ts">
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';

  let {
    currentQuantity,
    mode = $bindable<'delta' | 'set'>('delta'),
    value = $bindable(''),
    reason = $bindable(''),
  }: {
    currentQuantity?: number;
    mode?: 'delta' | 'set';
    value?: string;
    reason?: string;
  } = $props();

  function switchMode(m: 'delta' | 'set') {
    mode = m;
    value = '';
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === '=' && currentQuantity !== undefined) {
      e.preventDefault();
      switchMode('set');
    } else if ((e.key === '+' || e.key === '-') && mode === 'set') {
      switchMode('delta');
    }
  }
</script>

<div class="space-y-3">
  <!-- Mode toggle -->
  <div class="flex rounded-md border border-border overflow-hidden text-xs font-medium">
    <button
      type="button"
      onclick={() => switchMode('delta')}
      class="flex-1 px-3 py-1.5 transition-colors
        {mode === 'delta' ? 'bg-foreground text-background' : 'text-muted-foreground hover:text-foreground'}"
    >± Delta</button>
    <button
      type="button"
      onclick={() => switchMode('set')}
      disabled={currentQuantity === undefined}
      class="flex-1 px-3 py-1.5 border-l border-border transition-colors disabled:opacity-40
        {mode === 'set' ? 'bg-foreground text-background' : 'text-muted-foreground hover:text-foreground'}"
    >= Set to</button>
  </div>

  <div class="space-y-1">
    <Label for="adj-value">
      {#if mode === 'delta'}
        Delta <span class="text-muted-foreground text-xs">(positive or negative{currentQuantity !== undefined ? '; press = to switch to set' : ''})</span>
      {:else}
        New quantity <span class="text-muted-foreground text-xs">(current: {currentQuantity})</span>
      {/if}
    </Label>
    <Input
      id="adj-value"
      type="number"
      bind:value
      onkeydown={handleKeydown}
      placeholder={mode === 'delta' ? 'e.g. +10 or -5' : 'e.g. 42'}
    />
  </div>

  <div class="space-y-1">
    <Label for="adj-reason">Reason</Label>
    <Input id="adj-reason" bind:value={reason} placeholder="Optional reason" />
  </div>
</div>
