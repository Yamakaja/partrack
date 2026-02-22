<script lang="ts">
  import Topbar from '$lib/components/layout/Topbar.svelte';

  let { teamId }: { teamId: string } = $props();
</script>

<Topbar title="Builds" />
<div class="p-6">
  <p class="text-muted-foreground text-sm">Content coming soon.</p>
</div>
