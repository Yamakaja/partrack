<script lang="ts">
  import Topbar from '$lib/components/layout/Topbar.svelte';

  let { teamId, buildId }: { teamId: string; buildId: string } = $props();
</script>

<Topbar title="Build" />
<div class="p-6">
  <p class="text-muted-foreground text-sm">Content coming soon.</p>
</div>
