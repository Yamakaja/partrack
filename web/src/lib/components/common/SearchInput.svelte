<script lang="ts">
  import { cn } from '$lib/utils/cn.js';
  import { Search, X } from 'lucide-svelte';

  let {
    value = $bindable(''),
    placeholder = 'Search…',
    class: className = '',
    onchange,
  }: {
    value?: string;
    placeholder?: string;
    class?: string;
    onchange?: (v: string) => void;
  } = $props();

  let timer: ReturnType<typeof setTimeout>;

  function handleInput(e: Event) {
    const v = (e.target as HTMLInputElement).value;
    value = v;
    clearTimeout(timer);
    timer = setTimeout(() => onchange?.(v), 300);
  }

  function clear() {
    value = '';
    onchange?.('');
  }
</script>

<div class={cn('relative', className)}>
  <Search class="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
  <input
    type="search"
    {placeholder}
    {value}
    oninput={handleInput}
    class="flex h-9 w-full rounded-md border border-input bg-background pl-8 pr-8 py-1 text-sm shadow-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
  />
  {#if value}
    <button
      type="button"
      onclick={clear}
      class="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
    >
      <X class="h-4 w-4" />
    </button>
  {/if}
</div>
