<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { getPart, listParameters, listListings, enrichPart } from '$lib/api/parts.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import LifecycleBadge from '$lib/components/parts/LifecycleBadge.svelte';
  import ParameterTable from '$lib/components/parts/ParameterTable.svelte';
  import ListingTable from '$lib/components/parts/ListingTable.svelte';
  import { Tabs, TabsList, TabsTrigger, TabsContent } from '$lib/components/ui/tabs/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { navigate } from '$lib/router/index.svelte.js';
  import { ArrowLeft, Sparkles } from 'lucide-svelte';

  let { teamId, partId }: { teamId: string; partId: string } = $props();

  const qc = useQueryClient();
  let enriching = $state(false);

  let partQuery = createQuery(() => ({
    queryKey: ['part', partId],
    queryFn: () => getPart(partId),
  }));

  let paramsQuery = createQuery(() => ({
    queryKey: ['part-params', partId],
    queryFn: () => listParameters(partId),
  }));

  let listingsQuery = createQuery(() => ({
    queryKey: ['part-listings', partId],
    queryFn: () => listListings(partId),
  }));

  async function handleEnrich() {
    enriching = true;
    try {
      const result = await enrichPart(partId, teamId);
      pushToast('info', 'Enrichment started', `Job: ${result.job_id}`);
    } catch (e: any) {
      pushToast('error', 'Enrichment failed', e.message);
    } finally {
      enriching = false;
    }
  }
</script>

<div class="flex flex-col h-full">
  <Topbar
    title={partQuery.data?.mpn ?? 'Part Detail'}
    breadcrumbs={[
      { label: 'Parts', href: `/teams/${teamId}/parts` },
      { label: partQuery.data?.mpn ?? '…' },
    ]}
  >
    {#snippet actions()}
      <Button variant="outline" size="sm" onclick={() => navigate(`/teams/${teamId}/parts`)}>
        <ArrowLeft class="h-4 w-4 mr-1" /> Back
      </Button>
      <Button size="sm" disabled={enriching} onclick={handleEnrich}>
        <Sparkles class="h-4 w-4 mr-1" />
        {enriching ? 'Enriching…' : 'Enrich'}
      </Button>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6">
    {#if partQuery.isPending}
      <div class="space-y-3">
        <Skeleton class="h-6 w-48" />
        <Skeleton class="h-4 w-72" />
      </div>
    {:else if partQuery.data}
      {@const part = partQuery.data}
      <div class="space-y-6">
        <!-- Overview card -->
        <div class="rounded-lg border border-border bg-card p-4 space-y-3">
          <div class="flex items-start justify-between">
            <div>
              <div class="flex items-center gap-2">
                <h2 class="text-xl font-bold font-mono">{part.mpn ?? '—'}</h2>
                <LifecycleBadge status={part.lifecycle_status} />
                <Badge variant="outline" class="capitalize">{part.scope}</Badge>
              </div>
              <p class="text-muted-foreground mt-1">{part.description ?? '—'}</p>
            </div>
          </div>
          <div class="grid grid-cols-2 gap-x-8 gap-y-1 text-sm">
            <div><span class="text-muted-foreground">Manufacturer:</span> <span class="font-medium">{part.manufacturer ?? '—'}</span></div>
            <div><span class="text-muted-foreground">Category:</span> <span class="font-medium">{part.category ?? '—'}</span></div>
            <div><span class="text-muted-foreground">Package:</span> <span class="font-medium">{part.package ?? '—'}</span></div>
            {#if part.datasheet_url}
              <div><span class="text-muted-foreground">Datasheet:</span> <a href={part.datasheet_url} target="_blank" class="text-primary hover:underline">Link</a></div>
            {/if}
          </div>
        </div>

        <!-- Tabs -->
        <Tabs value="parameters">
          <TabsList>
            <TabsTrigger value="parameters">Parameters</TabsTrigger>
            <TabsTrigger value="listings">Listings ({listingsQuery.data?.items.length ?? '…'})</TabsTrigger>
          </TabsList>

          <TabsContent value="parameters">
            {#if paramsQuery.isPending}
              <Skeleton class="h-32 w-full" />
            {:else if paramsQuery.data}
              <ParameterTable
                {partId}
                parameters={paramsQuery.data}
                onchange={() => qc.invalidateQueries({ queryKey: ['part-params', partId] })}
              />
            {/if}
          </TabsContent>

          <TabsContent value="listings">
            {#if listingsQuery.isPending}
              <Skeleton class="h-32 w-full" />
            {:else if listingsQuery.data}
              <ListingTable
                {partId}
                {teamId}
                listings={listingsQuery.data.items}
                onrefresh={() => qc.invalidateQueries({ queryKey: ['part-listings', partId] })}
              />
            {/if}
          </TabsContent>
        </Tabs>
      </div>
    {/if}
  </div>
</div>
