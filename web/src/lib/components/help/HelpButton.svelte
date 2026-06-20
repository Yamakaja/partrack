<script lang="ts">
  import { CircleHelp } from 'lucide-svelte';
  import { openHelpForCurrentRoute } from '$lib/stores/help.svelte.js';
</script>

<button
  onclick={openHelpForCurrentRoute}
  title="Help for this page"
  aria-label="Help for this page"
  class="text-muted-foreground hover:text-foreground p-1.5 rounded hover:bg-accent/50 transition-colors"
>
  <CircleHelp class="size-4" />
</button>
