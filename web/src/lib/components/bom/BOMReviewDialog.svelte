<script lang="ts">
  import type { BOMLineItem } from '$lib/types/projects.js';
  import type { PartSummary } from '$lib/types/parts.js';
  import type { DistributorConfig } from '$lib/types/parts.js';
  import type { DistributorSearchProduct } from '$lib/api/distributors.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Dialog } from '$lib/components/ui/dialog/index.js';
  import { listDistributorConfigs, searchDistributor } from '$lib/api/distributors.js';
  import { listParts } from '$lib/api/parts.js';
  import { enrichBOMLineItem } from '$lib/api/revisions.js';
  import { ChevronLeft, ChevronRight } from 'lucide-svelte';

  let {
    items,
    teamId,
    projectId,
    revisionId,
    open = $bindable(false),
    onclose,
    onresolve,
    onignore,
  }: {
    items: BOMLineItem[];
    teamId: string;
    projectId: string;
    revisionId: string;
    open?: boolean;
    onclose?: () => void;
    onresolve?: (itemId: string, partId: string | null) => void;
    onignore?: (itemId: string, ignored: boolean) => void;
  } = $props();

  let currentIndex = $state(0);

  // Local library search state
  let localKeywords = $state('');
  let localResults = $state<PartSummary[]>([]);
  let localSearching = $state(false);
  let localError = $state('');
  let localSearchTimer: ReturnType<typeof setTimeout> | undefined;

  // Distributor search state
  let configs = $state<DistributorConfig[]>([]);
  let selectedDistributorId = $state('');
  let distributorKeywords = $state('');
  let distributorResults = $state<DistributorSearchProduct[]>([]);
  let distributorSearching = $state(false);
  let distributorError = $state('');
  let distributorImporting = $state(false);
  let configsLoaded = $state(false);

  // Reset state when dialog opens
  $effect(() => {
    if (open) {
      currentIndex = 0;
    }
  });

  $effect(() => {
    // Reset panel state whenever the current index changes or the dialog opens
    open;
    currentIndex;
    const keywords = currentItem?.mpn ?? currentItem?.part_name ?? '';
    localKeywords = keywords;
    localResults = [];
    localError = '';
    distributorResults = [];
    distributorError = '';
    distributorKeywords = currentItem?.raw_description ? (keywords || currentItem.raw_description) : keywords;
    clearTimeout(localSearchTimer);
    runLocalSearch(keywords);
  });

  $effect(() => {
    if (open && !configsLoaded) {
      listDistributorConfigs(teamId).then(r => {
        configs = r.items.filter(c => c.enabled && c.has_credentials);
      }).catch(() => {}).finally(() => {
        configsLoaded = true;
      });
    }
  });

  let currentItem = $derived(items[currentIndex] ?? null);
  let isLast = $derived(currentIndex >= items.length - 1);

  type BadgeVariant = 'default' | 'secondary' | 'destructive' | 'outline' | 'success' | 'warning';
  let statusBadge = $derived(
    !currentItem                              ? { label: '',           variant: 'default'   as BadgeVariant } :
    currentItem.ignored                       ? { label: 'Ignored',    variant: 'secondary' as BadgeVariant } :
    !currentItem.resolved_part_id
      ? currentItem.resolution_state === 'ambiguous'
        ? { label: 'Ambiguous',  variant: 'warning'   as BadgeVariant }
        : { label: 'Unresolved', variant: 'outline'   as BadgeVariant }
    : currentItem.resolution_state === 'auto_sku'           ? { label: 'Auto (SKU)',   variant: 'success' as BadgeVariant }
    : currentItem.resolution_state === 'auto_mpn'           ? { label: 'Auto (MPN)',   variant: 'success' as BadgeVariant }
    : currentItem.resolution_state === 'auto_prior_revision'? { label: 'Auto (Prior)', variant: 'success' as BadgeVariant }
    : currentItem.resolution_state === 'manual'             ? { label: 'Manual',       variant: 'success' as BadgeVariant }
    :                                               { label: 'Resolved',   variant: 'success' as BadgeVariant }
  );

  function advance() {
    if (isLast) {
      onclose?.();
    } else {
      currentIndex++;
    }
  }

  async function runLocalSearch(q: string) {
    if (q.trim().length < 2) return;
    localSearching = true;
    localError = '';
    localResults = [];
    try {
      const r = await listParts({ team_id: teamId, q: q.trim(), limit: 10, offset: 0 });
      localResults = r.items;
      if (r.items.length === 0) localError = 'No parts found.';
    } catch (err: any) {
      localError = err?.message ?? 'Search failed.';
    } finally {
      localSearching = false;
    }
  }

  function handleLocalImport(part: PartSummary) {
    if (!currentItem) return;
    onresolve?.(currentItem.id, part.id);
    advance();
  }

  function handleIgnore() {
    onignore?.(currentItem!.id, true);
    advance();
  }

  function handleUnignore() {
    onignore?.(currentItem!.id, false);
  }

  function handleClear() {
    onresolve?.(currentItem!.id, null);
  }

  async function handleDistributorSearch() {
    if (!selectedDistributorId || !distributorKeywords.trim()) return;
    distributorSearching = true;
    distributorError = '';
    distributorResults = [];
    try {
      const r = await searchDistributor(teamId, selectedDistributorId, distributorKeywords.trim());
      distributorResults = r.items;
      if (r.items.length === 0) distributorError = 'No results found.';
    } catch (err: any) {
      distributorError = err?.message ?? 'Search failed.';
    } finally {
      distributorSearching = false;
    }
  }

  async function handleImportSelected(result: DistributorSearchProduct) {
    if (!currentItem) return;
    distributorImporting = true;
    distributorError = '';
    try {
      const r = await enrichBOMLineItem(teamId, projectId, revisionId, currentItem.id, {
        sku: result.primary_sku,
        distributor_id: selectedDistributorId,
      });
      onresolve?.(currentItem.id, r.part_id);
      advance();
    } catch (err: any) {
      distributorError = err?.message ?? 'Import failed.';
    } finally {
      distributorImporting = false;
    }
  }
</script>

<Dialog bind:open class="max-w-2xl" title="BOM Review">
  {#snippet children()}
    <div class="flex flex-col min-h-[540px]">
    {#if !currentItem}
      <p class="text-sm text-muted-foreground">No items to review.</p>
      <div class="mt-4 flex justify-end">
        <Button size="sm" onclick={() => onclose?.()}>Close</Button>
      </div>
    {:else}
      <!-- Progress -->
      <div class="flex items-center justify-between mb-4">
        <span class="text-xs text-muted-foreground">Item {currentIndex + 1} of {items.length}</span>
        <Button variant="ghost" size="sm" onclick={() => onclose?.()}>Close</Button>
      </div>

      <!-- Item summary -->
      <div class="rounded-md border border-border p-4 space-y-2 mb-4">
        <div class="flex items-center gap-2 flex-wrap">
          <Badge variant={statusBadge.variant}>{statusBadge.label}</Badge>
          {#if currentItem.resolved_part_id && currentItem.resolved_part_mpn}
            <span class="font-mono text-xs text-muted-foreground">→ {currentItem.resolved_part_mpn}</span>
          {/if}
          {#if currentItem.resolved_part_id}
            <button
              type="button"
              class="text-xs text-muted-foreground hover:text-destructive transition-colors ml-auto"
              onclick={handleClear}
            >
              Clear resolution
            </button>
          {/if}
        </div>

        <div class="grid grid-cols-2 gap-x-4 gap-y-1 text-sm">
          {#if currentItem.part_name}
            <div><span class="text-muted-foreground">Name</span> <span class="ml-1">{currentItem.part_name}</span></div>
          {/if}
          {#if currentItem.manufacturer}
            <div><span class="text-muted-foreground">Mfr</span> <span class="ml-1">{currentItem.manufacturer}</span></div>
          {/if}
          {#if currentItem.mpn}
            <div><span class="text-muted-foreground">MPN</span> <span class="font-mono ml-1">{currentItem.mpn}</span></div>
          {/if}
          <div><span class="text-muted-foreground">Qty</span> <span class="ml-1 tabular-nums">{currentItem.quantity}</span></div>
          {#if currentItem.reference_designators}
            <div class="col-span-2"><span class="text-muted-foreground">Refs</span> <span class="font-mono text-xs ml-1">{currentItem.reference_designators}</span></div>
          {/if}
          {#if currentItem.raw_description}
            <div class="col-span-2"><span class="text-muted-foreground">Description</span> <span class="ml-1 text-muted-foreground">{currentItem.raw_description}</span></div>
          {/if}
          {#if currentItem.distributor_name}
            <div>
              <span class="text-muted-foreground">Distributor</span>
              <span class="ml-1">{currentItem.distributor_name}
                <span class="text-xs">{currentItem.has_linked_distributor ? '✓' : '✕'}</span>
              </span>
            </div>
          {/if}
          {#if currentItem.sku}
            <div><span class="text-muted-foreground">SKU</span> <span class="font-mono ml-1">{currentItem.sku}</span></div>
          {/if}
        </div>
      </div>

      <!-- Actions -->
      <div class="space-y-2 flex-1">
        <!-- Choose from local library -->
        <div class="border border-border rounded-md">
          <div class="px-3 py-2 text-sm font-medium">Choose from local library</div>
          <div class="px-3 pb-3 pt-1 space-y-2">
            <input
              type="text"
              bind:value={localKeywords}
              placeholder="Search parts…"
              class="h-9 w-full rounded-md border border-input bg-background px-3 py-1 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              oninput={() => { clearTimeout(localSearchTimer); localSearchTimer = setTimeout(() => runLocalSearch(localKeywords), 300); }}
            />

            {#if localError}
              <p class="text-xs text-destructive">{localError}</p>
            {/if}

            {#if localResults.length > 0}
              <div class="max-h-48 overflow-y-auto border border-border rounded-md divide-y divide-border">
                {#each localResults as part (part.id)}
                  <button
                    type="button"
                    class="w-full text-left px-3 py-2 text-sm hover:bg-accent transition-colors"
                    onclick={() => handleLocalImport(part)}
                  >
                    <div class="flex items-center gap-2">
                      <span class="font-medium font-mono">{part.mpn ?? '—'}</span>
                      {#if part.manufacturer}
                        <span class="text-muted-foreground">{part.manufacturer}</span>
                      {/if}
                      {#if part.package}
                        <span class="text-xs text-muted-foreground">{part.package}</span>
                      {/if}
                    </div>
                    {#if part.description}
                      <div class="text-xs text-muted-foreground truncate mt-0.5">{part.description}</div>
                    {/if}
                  </button>
                {/each}
              </div>
            {/if}
          </div>
        </div>

        <!-- Import from distributor -->
        <div class="border border-border rounded-md">
          <div class="px-3 py-2 text-sm font-medium">Import from distributor</div>
          <div class="px-3 pb-3 pt-1 space-y-2">
            {#if configs.length === 0}
              <p class="text-xs text-muted-foreground">No distributor integrations configured for this team.</p>
            {:else}
              {#if selectedDistributorId}
                <!-- Selected distributor header -->
                <div class="flex items-center gap-1.5">
                  <button
                    type="button"
                    class="text-muted-foreground hover:text-foreground transition-colors"
                    onclick={() => { selectedDistributorId = ''; distributorResults = []; distributorError = ''; }}
                  >
                    <ChevronLeft class="h-4 w-4" />
                  </button>
                  <span class="text-sm font-medium">
                    {configs.find(c => c.distributor_id === selectedDistributorId)?.distributor_name}
                  </span>
                </div>
                <div class="flex gap-2">
                  <input
                    type="text"
                    bind:value={distributorKeywords}
                    placeholder="Search keywords…"
                    class="h-9 flex-1 rounded-md border border-input bg-background px-3 py-1 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    onkeydown={(e) => { if (e.key === 'Enter') handleDistributorSearch(); }}
                  />
                  <Button
                    size="sm"
                    variant="outline"
                    onclick={handleDistributorSearch}
                    disabled={distributorSearching || !distributorKeywords.trim()}
                  >
                    {distributorSearching ? 'Searching…' : 'Search'}
                  </Button>
                </div>
              {:else}
                <!-- Distributor picker -->
                <div class="grid gap-2">
                  {#each configs as cfg (cfg.distributor_id)}
                    <button
                      type="button"
                      class="w-full py-2.5 text-sm font-medium rounded-md border border-border hover:bg-accent transition-colors text-center"
                      onclick={() => { selectedDistributorId = cfg.distributor_id; }}
                    >
                      {cfg.distributor_name}
                    </button>
                  {/each}
                </div>
              {/if}

              {#if distributorError}
                <p class="text-xs text-destructive">{distributorError}</p>
              {/if}

              {#if distributorResults.length > 0}
                <div class="max-h-48 overflow-y-auto border border-border rounded-md divide-y divide-border">
                  {#each distributorResults as result (result.primary_sku)}
                    <button
                      type="button"
                      class="w-full text-left px-3 py-2 text-sm hover:bg-accent transition-colors disabled:opacity-50"
                      disabled={distributorImporting}
                      onclick={() => handleImportSelected(result)}
                    >
                      <div class="flex items-center justify-between gap-2">
                        <div>
                          <span class="font-medium font-mono">{result.mpn}</span>
                          {#if result.manufacturer}
                            <span class="text-muted-foreground ml-1">{result.manufacturer}</span>
                          {/if}
                          {#if result.package}
                            <span class="text-xs text-muted-foreground ml-1">{result.package}</span>
                          {/if}
                        </div>
                        <div class="text-xs text-muted-foreground text-right shrink-0">
                          {#if result.stock_available != null}
                            <span>{result.stock_available.toLocaleString()} in stock</span>
                          {/if}
                          {#if result.unit_price}
                            <span class="ml-1">${result.unit_price}</span>
                          {/if}
                        </div>
                      </div>
                      {#if result.description}
                        <div class="text-xs text-muted-foreground truncate mt-0.5">{result.description}</div>
                      {/if}
                      <div class="text-xs text-muted-foreground mt-0.5">SKU: {result.primary_sku}</div>
                    </button>
                  {/each}
                </div>
              {/if}
            {/if}
          </div>
        </div>

      </div>

      <!-- Navigation -->
      <div class="flex items-center justify-between mt-6 pt-4 border-t border-border">
        <Button
          variant="ghost"
          size="sm"
          onclick={() => { currentIndex = Math.max(0, currentIndex - 1); }}
          disabled={currentIndex === 0}
        >
          <ChevronLeft class="h-4 w-4 mr-1" /> Prev
        </Button>
        <div class="flex items-center gap-2">
          {#if currentItem.ignored}
            <Button variant="outline" size="sm" class="text-amber-600 border-amber-600 hover:bg-amber-50 hover:text-amber-700" onclick={handleUnignore}>
              Un-ignore
            </Button>
          {:else}
            <Button variant="outline" size="sm" onclick={handleIgnore}>
              Ignore
            </Button>
          {/if}
          <Button
            variant="ghost"
            size="sm"
            onclick={advance}
          >
            {isLast ? 'Done' : 'Skip'} <ChevronRight class="h-4 w-4 ml-1" />
          </Button>
        </div>
      </div>
    {/if}
    </div>
  {/snippet}
</Dialog>
