<script lang="ts">
  import { Check, Minus } from 'lucide-svelte';
  import { cn } from '$lib/utils/cn.js';

  // Tri-state checkbox. `indeterminate` is the "some selected" dash and maps to
  // aria-checked="mixed"; clicking it selects all rather than clearing, which is
  // what every checkable list does.
  //
  // The click event rides along in onchange so callers can implement
  // shift-click range selection without a second listener.
  let {
    checked = false,
    indeterminate = false,
    disabled = false,
    label,
    class: className = '',
    onchange,
  }: {
    checked?: boolean;
    indeterminate?: boolean;
    disabled?: boolean;
    /** Accessible name — the box stands alone, so this becomes its aria-label. */
    label: string;
    class?: string;
    onchange?: (checked: boolean, event: MouseEvent) => void;
  } = $props();
</script>

<button
  type="button"
  role="checkbox"
  aria-checked={indeterminate ? 'mixed' : checked}
  aria-label={label}
  title={label}
  {disabled}
  onclick={(e) => onchange?.(indeterminate ? true : !checked, e)}
  class={cn(
    'flex h-4 w-4 shrink-0 items-center justify-center rounded-[4px] border transition-colors',
    'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-1 focus-visible:ring-offset-background',
    checked || indeterminate
      ? 'border-primary bg-primary text-primary-foreground'
      : 'border-input hover:border-muted-foreground/60',
    disabled && 'pointer-events-none opacity-40',
    className,
  )}
>
  {#if indeterminate}
    <Minus class="h-3 w-3" />
  {:else if checked}
    <Check class="h-3 w-3" />
  {/if}
</button>
