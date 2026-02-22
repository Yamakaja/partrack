<script lang="ts">
  import Topbar from '$lib/components/layout/Topbar.svelte';

  let { teamId }: { teamId: string } = $props();
</script>

<Topbar title="Dashboard" />
<div class="grid grid-cols-2 gap-4 p-6">
  <div class="bg-card rounded-lg border p-6">
    <p class="text-muted-foreground text-sm">Total Parts</p>
    <p class="text-3xl font-bold mt-2">—</p>
  </div>
  <div class="bg-card rounded-lg border p-6">
    <p class="text-muted-foreground text-sm">Low Stock Items</p>
    <p class="text-3xl font-bold mt-2">—</p>
  </div>
  <div class="bg-card rounded-lg border p-6">
    <p class="text-muted-foreground text-sm">Active Projects</p>
    <p class="text-3xl font-bold mt-2">—</p>
  </div>
  <div class="bg-card rounded-lg border p-6">
    <p class="text-muted-foreground text-sm">Pending Orders</p>
    <p class="text-3xl font-bold mt-2">—</p>
  </div>
</div>
