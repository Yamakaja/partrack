<script lang="ts">
  import PartsTable from '$lib/components/parts/PartsTable.svelte';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import AddPartDialog from '$lib/components/part-import/AddPartDialog.svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { consumePartsStockStatus } from '$lib/stores/navState.js';
  import { Plus } from 'lucide-svelte';

  let { teamId }: { teamId: string } = $props();

  let partsTable = $state<ReturnType<typeof PartsTable>>();
  let createOpen = $state(false);

  const initialStockStatus = consumePartsStockStatus();
</script>

<div class="flex flex-col h-full">
  <Topbar title="Parts">
  {#snippet actions()}
    <Button size="sm" onclick={() => (createOpen = true)}>
      <Plus class="h-4 w-4 mr-1" /> Add Part
    </Button>
  {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6">
    <PartsTable
      bind:this={partsTable}
      {teamId}
      onRowClick={(p) => navigate(`/teams/${teamId}/parts/${p.id}`)}
      initialStockFilter={initialStockStatus ?? undefined}
    />
  </div>
</div>

<!-- Add Part Dialog -->
<AddPartDialog bind:open={createOpen} {teamId} onCommitted={() => partsTable?.refresh()} />
