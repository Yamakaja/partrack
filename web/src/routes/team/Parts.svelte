<script lang="ts">
  import { reenrichAllParts } from '$lib/api/parts.js';
  import PartsTable from '$lib/components/parts/PartsTable.svelte';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import AddPartDialog from '$lib/components/part-import/AddPartDialog.svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Dialog } from '$lib/components/ui/dialog/index.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { consumePartsStockStatus } from '$lib/stores/navState.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { authStore } from '$lib/stores/auth.svelte.js';
  import { Plus, RefreshCw } from 'lucide-svelte';

  let { teamId }: { teamId: string } = $props();

  let partsTable = $state<ReturnType<typeof PartsTable>>();
  let reenriching = $state(false);
  let reenrichConfirmOpen = $state(false);
  let createOpen = $state(false);

  const initialStockStatus = consumePartsStockStatus();

  async function doReenrichAll() {
    reenrichConfirmOpen = false;
    reenriching = true;
    try {
      const res = await reenrichAllParts(teamId);
      pushToast('success', 'Re-enrichment queued', `${res.job_count} part${res.job_count === 1 ? '' : 's'} queued for enrichment.`);
    } catch (err: any) {
      pushToast('error', 'Re-enrichment failed', err.message);
    } finally {
      reenriching = false;
    }
  }
</script>

<div class="flex flex-col h-full">
  <Topbar title="Parts">
  {#snippet actions()}
    {#if authStore.user?.is_admin}
      <Button size="sm" variant="outline" disabled={reenriching} onclick={() => (reenrichConfirmOpen = true)}>
        <RefreshCw class="h-4 w-4 mr-1" />
        {reenriching ? 'Enriching…' : 'Re-enrich All'}
      </Button>
    {/if}
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

<!-- Re-enrich All Confirmation Dialog -->
<Dialog
  bind:open={reenrichConfirmOpen}
  title="Re-enrich all parts?"
  confirmLabel="Re-enrich All"
  onConfirm={doReenrichAll}
>
  {#snippet children()}
    <div class="text-sm text-muted-foreground space-y-2">
      <p>This will queue an enrichment job for every part in this team's inventory that has a distributor listing. Each job makes a live API call to the distributor.</p>
      <p>For large inventories this may take a while to process and will count against your distributor API rate limits.</p>
    </div>
  {/snippet}
</Dialog>

<!-- Add Part Dialog -->
<AddPartDialog bind:open={createOpen} {teamId} onCommitted={() => partsTable?.refresh()} />
