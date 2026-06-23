<script lang="ts">
  import { cn } from '$lib/utils/cn.js';

  // The partrack mark: a 2x2 bin rack with the top-left bin filled ("part rack",
  // filled cell = stock). Single-path currentColor SVG on the 24-unit Lucide grid,
  // so it themes itself anywhere it inherits text color. Pass `accent` to fill the
  // bin with the brand amber (in-stock) instead of currentColor.
  let {
    class: className = 'size-5',
    accent = false,
  }: { class?: string; accent?: boolean } = $props();
</script>

<svg
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="2"
  stroke-linejoin="round"
  class={cn(className)}
  aria-hidden="true"
>
  <path
    d="M12 3H6a3 3 0 0 0-3 3v6h9V3Z"
    fill={accent ? 'hsl(38 92% 50%)' : 'currentColor'}
    stroke="none"
  />
  <rect x="3" y="3" width="18" height="18" rx="3" />
  <line x1="12" y1="3" x2="12" y2="21" />
  <line x1="3" y1="12" x2="21" y2="12" />
</svg>
