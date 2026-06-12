<script lang="ts">
  import { Cpu } from 'lucide-svelte';

  // Rendered both inside the editor (as a ProseMirror node-view) and, later,
  // in read-only note display. In the spike it shows the stored label; the real
  // feature will resolve `id` against the manifest for live MPN/lifecycle/stock.
  let { label, id }: { label: string; id: string } = $props();
</script>

<span
  class="inline-flex items-center gap-1 rounded border border-primary/30 bg-primary/10 px-1.5 py-0.5 align-baseline text-sm font-medium text-primary"
  title={id}
  contenteditable="false"
>
  <Cpu class="h-3 w-3 shrink-0" />
  {label}
</span>
