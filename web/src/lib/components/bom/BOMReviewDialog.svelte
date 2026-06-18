<script lang="ts">
  import type { BOMLineItem } from '$lib/types/projects.js';
  import type { PartSummary } from '$lib/types/parts.js';
  import type { DistributorConfig } from '$lib/types/parts.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Dialog } from '$lib/components/ui/dialog/index.js';
  import { listDistributorConfigs } from '$lib/api/distributors.js';
  import { listParts } from '$lib/api/parts.js';
  import { listGenericCandidates, type GenericCandidate } from '$lib/api/revisions.js';
  import { createPartResolveItem, getPartResolveItem, commitResolveItem, type PartResolveItemFull } from '$lib/api/partResolve.js';
  import { ChevronLeft, ChevronRight } from 'lucide-svelte';
  import ListingSelector from '$lib/components/part-import/ListingSelector.svelte';
  import InterpretationEditor from '$lib/components/bom/InterpretationEditor.svelte';

  let {
    items,
    teamId,
    projectId,
    revisionId,
    open = $bindable(false),
    onclose,
    onresolve,
    onignore,
    oncommitted,
    oncreategeneric,
  }: {
    items: BOMLineItem[];
    teamId: string;
    projectId: string;
    revisionId: string;
    open?: boolean;
    onclose?: () => void;
    onresolve?: (itemId: string, partId: string | null) => void;
    onignore?: (itemId: string, ignored: boolean) => void;
    oncommitted?: (itemId: string) => void;
    /** Create-or-get a generic part from the line's parametric interpretation.
        Resolves true on success so the dialog can advance. */
    oncreategeneric?: (item: BOMLineItem) => Promise<boolean>;
  } = $props();

  // New-part import (resolve pipeline) selection state.
  let primaryListingKey = $state('');
  let includedListings = $state<Set<string>>(new Set());
  let committing = $state(false);
  let commitError = $state('');

  function parseListingKey(key: string): { distId: string; sku: string } {
    return { distId: key.slice(0, 36), sku: key.slice(37) };
  }

  let currentIndex = $state(0);

  // Local library search state
  let localKeywords = $state('');
  let localResults = $state<PartSummary[]>([]);
  let localSearching = $state(false);
  let localError = $state('');
  let localSearchTimer: ReturnType<typeof setTimeout> | undefined;

  // Library candidate picker (matches the line's generic spec) state.
  let candidates = $state<GenericCandidate[]>([]);
  let candidatesLoading = $state(false);
  let candidatesInStock = $derived(candidates.filter((c) => (c.quantity_on_hand ?? 0) > 0).length);

  let configs = $state<DistributorConfig[]>([]);
  let configsLoaded = $state(false);

  // Distributor import via the resolve pipeline: one lookup fans out across all
  // enabled distributors (same background job as Add Part and the BOM importer),
  // then the user picks the listing(s) and commits — instead of the old
  // pick-one-distributor + keyword-search + single-SKU enrich flow.
  type DistStep = 'entry' | 'resolving' | 'review';
  let distStep = $state<DistStep>('entry');
  let distSku = $state('');
  let distDistId = $state('');
  let distMpn = $state('');
  let distMfr = $state('');
  let distResolveItemId = $state<string | null>(null);
  let distResolve = $state<PartResolveItemFull | null>(null);
  let distError = $state('');
  let distCommitting = $state(false);
  let distPrimaryListingKey = $state('');
  let distIncludedListings = $state<Set<string>>(new Set());
  let distPollTimer: ReturnType<typeof setTimeout> | undefined;

  let distEnrichment = $derived(distResolve?.enrichment_data ?? null);
  let distHasSku = $derived(distSku.trim() !== '' && distDistId !== '');
  let distHasMpn = $derived(distMpn.trim() !== '');
  let distCanResolve = $derived(distHasSku || distHasMpn);

  function resetDistImport() {
    clearTimeout(distPollTimer);
    distStep = 'entry';
    distResolveItemId = null;
    distResolve = null;
    distError = '';
    distCommitting = false;
    distPrimaryListingKey = '';
    distIncludedListings = new Set();
  }

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
    clearTimeout(localSearchTimer);
    runLocalSearch(keywords);
    // Reset new-part import selection so ListingSelector re-initialises.
    primaryListingKey = '';
    includedListings = new Set();
    commitError = '';
    // Reset the distributor-import sub-flow and prefill from the line's identifiers.
    resetDistImport();
    distMpn = currentItem?.mpn ?? '';
    distSku = currentItem?.sku ?? '';
    distMfr = currentItem?.manufacturer ?? '';
    distDistId = '';
    // Spec-based candidates for an unresolved generic line.
    candidates = [];
    if (currentItem && !currentItem.resolved_part_id && currentItem.interpretation?.route === 'parametric') {
      loadCandidates(currentItem.id);
    }
  });

  async function loadCandidates(itemId: string) {
    candidatesLoading = true;
    try {
      const r = await listGenericCandidates(teamId, projectId, revisionId, itemId);
      // Guard against a stale response after the user has navigated on.
      if (currentItem?.id === itemId) candidates = r.items;
    } catch {
      if (currentItem?.id === itemId) candidates = [];
    } finally {
      candidatesLoading = false;
    }
  }

  function formatSi(v: number | null): string {
    if (v == null) return '';
    const abs = Math.abs(v);
    const units: [number, string][] = [[1e9, 'G'], [1e6, 'M'], [1e3, 'k'], [1, ''], [1e-3, 'm'], [1e-6, 'µ'], [1e-9, 'n'], [1e-12, 'p']];
    for (const [scale, prefix] of units) {
      if (abs >= scale) {
        const n = v / scale;
        return `${Number.isInteger(n) ? n : parseFloat(n.toFixed(3))}${prefix}`;
      }
    }
    return String(v);
  }

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

  let preferredDistId = $derived(configs.find(c => c.preferred)?.distributor_id ?? null);

  // A new-part import that the worker enriched but could not auto-commit
  // (multiple candidate products). The user picks the listing(s) here.
  let newPartEnrichment = $derived(
    currentItem?.resolve_item?.status === 'enriched' &&
    currentItem.resolve_item.task_type === 'unknown_part_import'
      ? (currentItem.resolve_item.enrichment_data ?? null)
      : null,
  );

  // The parser's structured reading of the line — the identity for generic
  // (no MPN/manufacturer) parts. Drives the create-generic action below.
  let interpretation = $derived(currentItem?.interpretation ?? null);
  let isParametric = $derived(interpretation?.route === 'parametric');

  // Resolved by a fuzzy (substring) MPN match — resolved but flagged for a confirm.
  let isFuzzyResolved = $derived(
    !!currentItem?.resolved_part_id && currentItem.resolution_state === 'auto_mpn_fuzzy',
  );

  let creatingGeneric = $state(false);
  let genericError = $state('');

  async function handleCreateGeneric() {
    if (!currentItem || !oncreategeneric) return;
    creatingGeneric = true;
    genericError = '';
    try {
      if (await oncreategeneric(currentItem)) {
        advance();
      }
    } catch (err: any) {
      genericError = err?.message ?? 'Failed to create generic part.';
    } finally {
      creatingGeneric = false;
    }
  }

  async function handleCommitNewPart() {
    const ri = currentItem?.resolve_item;
    if (!ri || !primaryListingKey) return;
    committing = true;
    commitError = '';
    try {
      const { distId, sku } = parseListingKey(primaryListingKey);
      await commitResolveItem(teamId, ri.id, {
        primary_distributor_id: distId,
        primary_sku: sku,
        additional_listings: [...includedListings].map((key) => {
          const p = parseListingKey(key);
          return { distributor_id: p.distId, sku: p.sku };
        }),
      });
      oncommitted?.(currentItem!.id);
      advance();
    } catch (err: any) {
      commitError = err?.message ?? 'Failed to import part.';
    } finally {
      committing = false;
    }
  }

  type BadgeVariant = 'default' | 'secondary' | 'destructive' | 'outline' | 'success' | 'warning';
  let statusBadge = $derived(
    !currentItem                              ? { label: '',           variant: 'default'   as BadgeVariant } :
    currentItem.ignored                       ? { label: 'Ignored',    variant: 'secondary' as BadgeVariant } :
    !currentItem.resolved_part_id
      ? currentItem.resolution_state === 'import_ambiguous' || currentItem.resolution_state === 'ambiguous'
        ? { label: 'Needs review', variant: 'warning'   as BadgeVariant }
        : currentItem.resolution_state === 'importing'
        ? { label: 'Importing…',   variant: 'secondary' as BadgeVariant }
        : currentItem.resolution_state === 'import_no_result'
        ? { label: 'No result',    variant: 'destructive' as BadgeVariant }
        : { label: 'Unresolved',   variant: 'outline'   as BadgeVariant }
    : currentItem.resolution_state === 'auto_mpn_fuzzy'     ? { label: 'Auto (MPN ?)', variant: 'warning' as BadgeVariant }
    : currentItem.resolution_state === 'auto_sku'           ? { label: 'Auto (SKU)',   variant: 'success' as BadgeVariant }
    : currentItem.resolution_state === 'auto_mpn'           ? { label: 'Auto (MPN)',   variant: 'success' as BadgeVariant }
    : currentItem.resolution_state === 'auto_generic'       ? { label: 'Auto (Generic)', variant: 'success' as BadgeVariant }
    : currentItem.resolution_state === 'auto_prior_revision'? { label: 'Auto (Prior)', variant: 'success' as BadgeVariant }
    : currentItem.resolution_state === 'auto_import'        ? { label: 'Imported',     variant: 'success' as BadgeVariant }
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
    if (currentItem.ignored) onignore?.(currentItem.id, false);
    onresolve?.(currentItem.id, part.id);
    advance();
  }

  function handleUseCandidate(c: GenericCandidate) {
    if (!currentItem) return;
    if (currentItem.ignored) onignore?.(currentItem.id, false);
    onresolve?.(currentItem.id, c.part_id);
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

  // Accept a fuzzy auto-match: re-resolve to the same part so it persists as a
  // confirmed ('manual') resolution and drops out of the review queue.
  function handleConfirmMatch() {
    if (!currentItem?.resolved_part_id) return;
    onresolve?.(currentItem.id, currentItem.resolved_part_id);
    advance();
  }

  // Kick off a resolve job that fans out across all enabled distributors.
  async function doDistResolve() {
    if (!distCanResolve || distStep === 'resolving') return;
    distError = '';
    distResolve = null;
    distPrimaryListingKey = '';
    distIncludedListings = new Set();
    try {
      const { resolve_item_id } = await createPartResolveItem(teamId, {
        sku: distHasSku ? distSku.trim() : undefined,
        distributor_id: distHasSku ? distDistId : undefined,
        mpn: distHasMpn ? distMpn.trim() : undefined,
        manufacturer: distMfr.trim() || undefined,
      });
      distResolveItemId = resolve_item_id;
      distStep = 'resolving';
      pollDistResolve();
    } catch (err: any) {
      distError = err?.message ?? 'Lookup failed.';
    }
  }

  // Poll the resolve item until it leaves 'pending'. The guard against a changed
  // resolveItemId drops stale polls after the user navigates to another line.
  function pollDistResolve() {
    clearTimeout(distPollTimer);
    const id = distResolveItemId;
    if (!id) return;
    distPollTimer = setTimeout(async () => {
      try {
        const r = await getPartResolveItem(teamId, id);
        if (distResolveItemId !== id) return;
        distResolve = r;
        if (r.status === 'pending') {
          pollDistResolve();
        } else if (r.resolved_part_id) {
          // The lookup matched a part already in the catalog — resolve straight to it.
          if (currentItem?.ignored) onignore?.(currentItem.id, false);
          onresolve?.(currentItem!.id, r.resolved_part_id);
          resetDistImport();
          advance();
        } else if (r.task_type === 'unknown_part_import' && r.enrichment_data) {
          distStep = 'review';
        } else {
          distError = r.error_message ?? 'No distributor match was found.';
          distStep = 'entry';
        }
      } catch (err: any) {
        if (distResolveItemId !== id) return;
        distError = err?.message ?? 'Lookup failed.';
        distStep = 'entry';
      }
    }, 1000);
  }

  // Commit the picked listing(s); the new part resolves this line (source 'manual').
  async function handleCommitDistImport() {
    if (!distResolveItemId || !distPrimaryListingKey || !currentItem) return;
    distCommitting = true;
    distError = '';
    try {
      const { distId, sku } = parseListingKey(distPrimaryListingKey);
      const { part_id } = await commitResolveItem(teamId, distResolveItemId, {
        primary_distributor_id: distId,
        primary_sku: sku,
        additional_listings: [...distIncludedListings].map((key) => {
          const p = parseListingKey(key);
          return { distributor_id: p.distId, sku: p.sku };
        }),
      });
      if (currentItem.ignored) onignore?.(currentItem.id, false);
      onresolve?.(currentItem.id, part_id);
      resetDistImport();
      advance();
    } catch (err: any) {
      distError = err?.message ?? 'Import failed.';
    } finally {
      distCommitting = false;
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

        {#if isFuzzyResolved}
          <div class="flex items-center gap-2 rounded-md border border-amber-500/40 bg-amber-500/5 px-3 py-2 text-xs">
            <span class="flex-1 text-muted-foreground">
              Matched to a similar MPN in your library (not an exact match) — confirm it's correct, or pick another below.
            </span>
            <Button size="sm" onclick={handleConfirmMatch}>Confirm match</Button>
          </div>
        {/if}

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

        <!-- How the parser read this line — the identity for generic parts -->
        {#if interpretation}
          <div class="rounded-md border border-border bg-muted/20 p-3">
            <InterpretationEditor
              interpretation={interpretation}
              source={{
                reference_designators: currentItem.reference_designators,
                value: currentItem.value,
                footprint: currentItem.footprint,
                mpn: currentItem.mpn,
                sku: currentItem.sku,
                manufacturer: currentItem.manufacturer,
              }}
              {teamId}
              readonly
            />
          </div>
        {/if}
      </div>

      <!-- Actions -->
      <div class="space-y-2 flex-1">
        <!-- Matching library parts (same spec the importer matched on) -->
        {#if isParametric && !currentItem.resolved_part_id && (candidatesLoading || candidates.length > 0)}
          <div class="border border-primary/40 rounded-md">
            <div class="px-3 py-2 text-sm font-medium">Matching parts in your library</div>
            <div class="px-3 pb-3 pt-1 space-y-2">
              {#if candidatesLoading}
                <p class="text-xs text-muted-foreground">Searching the catalog…</p>
              {:else}
                <p class="text-xs text-muted-foreground">
                  {candidates.length} part{candidates.length === 1 ? '' : 's'} match this line's spec
                  {#if candidatesInStock > 0}<span class="text-foreground">· {candidatesInStock} in stock</span>{/if}.
                  Pick one to resolve it.
                </p>
                <div class="max-h-48 overflow-y-auto border border-border rounded-md divide-y divide-border">
                  {#each candidates as c (c.part_id)}
                    <button
                      type="button"
                      class="w-full text-left px-3 py-2 text-sm hover:bg-accent transition-colors"
                      onclick={() => handleUseCandidate(c)}
                    >
                      <div class="flex items-center gap-2">
                        <span class="font-medium font-mono">{c.mpn ?? '—'}</span>
                        {#if c.manufacturer}<span class="text-muted-foreground">{c.manufacturer}</span>{/if}
                        {#if c.value_si != null}<Badge variant="outline">{formatSi(c.value_si)}</Badge>{/if}
                        {#if c.package}<span class="text-xs text-muted-foreground">{c.package}</span>{/if}
                        <span class="ml-auto flex items-center gap-2">
                          {#if c.quantity_on_hand == null}
                            <span class="text-[10px] text-muted-foreground">no stock entry</span>
                          {:else if c.quantity_on_hand > 0}
                            <Badge variant="success">{c.quantity_on_hand.toLocaleString()} in stock</Badge>
                          {:else}
                            <Badge variant="secondary">0 in stock</Badge>
                          {/if}
                          {#if c.scope === 'global'}<span class="text-[10px] text-muted-foreground">global</span>{/if}
                        </span>
                      </div>
                      {#if c.description}
                        <div class="text-xs text-muted-foreground truncate mt-0.5">{c.description}</div>
                      {/if}
                    </button>
                  {/each}
                </div>
              {/if}
            </div>
          </div>
        {/if}

        <!-- Create generic part (parser read this line as a value/footprint spec) -->
        {#if isParametric && oncreategeneric && !currentItem.resolved_part_id}
          <div class="border border-primary/40 rounded-md">
            <div class="px-3 py-2 text-sm font-medium">Create generic part</div>
            <div class="px-3 pb-3 pt-1 space-y-2">
              <p class="text-xs text-muted-foreground">
                This line has no specific manufacturer part. Create (or reuse) a generic
                part from the interpreted spec above and resolve this line to it.
              </p>
              {#if genericError}
                <p class="text-xs text-destructive">{genericError}</p>
              {/if}
              <div class="flex justify-end">
                <Button size="sm" onclick={handleCreateGeneric} disabled={creatingGeneric}>
                  {creatingGeneric ? 'Creating…' : 'Create & resolve'}
                </Button>
              </div>
            </div>
          </div>
        {/if}

        <!-- Import new part (enriched by the resolve pipeline, needs a listing pick) -->
        {#if newPartEnrichment}
          <div class="border border-primary/40 rounded-md">
            <div class="px-3 py-2 text-sm font-medium">Import new part</div>
            <div class="px-3 pb-3 pt-1 space-y-3">
              <p class="text-xs text-muted-foreground">
                Multiple matching products were found. Pick the primary listing (and any others to import), then confirm.
              </p>
              <ListingSelector
                enrichment={newPartEnrichment}
                {preferredDistId}
                bind:primaryListingKey
                bind:includedListings
              />
              {#if commitError}
                <p class="text-xs text-destructive">{commitError}</p>
              {/if}
              <div class="flex justify-end">
                <Button size="sm" onclick={handleCommitNewPart} disabled={committing || !primaryListingKey}>
                  {committing ? 'Importing…' : 'Import & resolve'}
                </Button>
              </div>
            </div>
          </div>
        {/if}

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

        <!-- Import from distributor (one lookup fans out across all enabled distributors) -->
        <div class="border border-border rounded-md">
          <div class="px-3 py-2 text-sm font-medium">Import from distributor</div>
          <div class="px-3 pb-3 pt-1 space-y-2">
            {#if configs.length === 0}
              <p class="text-xs text-muted-foreground">No distributor integrations configured for this team.</p>
            {:else if distStep === 'entry'}
              <p class="text-xs text-muted-foreground">
                Look up this part by MPN or distributor SKU. We'll search all enabled distributors and let you pick the listing.
              </p>
              <div class="grid grid-cols-[1fr_1.4fr] gap-2">
                <div class="space-y-1">
                  <label class="text-xs text-muted-foreground" for="dist-imp-dist">Distributor</label>
                  <select
                    id="dist-imp-dist"
                    bind:value={distDistId}
                    class="h-9 w-full rounded-md border border-input bg-background px-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    <option value="">— Select —</option>
                    {#each configs as cfg (cfg.distributor_id)}
                      <option value={cfg.distributor_id}>{cfg.distributor_name}{cfg.preferred ? ' (preferred)' : ''}</option>
                    {/each}
                  </select>
                </div>
                <div class="space-y-1">
                  <label class="text-xs text-muted-foreground" for="dist-imp-sku">SKU</label>
                  <input
                    id="dist-imp-sku"
                    type="text"
                    bind:value={distSku}
                    placeholder="distributor SKU"
                    class="h-9 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  />
                </div>
              </div>
              <div class="relative flex items-center gap-2 text-[10px] uppercase tracking-wide text-muted-foreground">
                <span class="h-px flex-1 bg-border"></span>and / or<span class="h-px flex-1 bg-border"></span>
              </div>
              <div class="grid grid-cols-2 gap-2">
                <div class="space-y-1">
                  <label class="text-xs text-muted-foreground" for="dist-imp-mpn">MPN</label>
                  <input
                    id="dist-imp-mpn"
                    type="text"
                    bind:value={distMpn}
                    placeholder="manufacturer part #"
                    class="h-9 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                    onkeydown={(e) => { if (e.key === 'Enter' && distCanResolve) doDistResolve(); }}
                  />
                </div>
                <div class="space-y-1">
                  <label class="text-xs text-muted-foreground" for="dist-imp-mfr">Manufacturer</label>
                  <input
                    id="dist-imp-mfr"
                    type="text"
                    bind:value={distMfr}
                    placeholder="optional"
                    class="h-9 w-full rounded-md border border-input bg-background px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  />
                </div>
              </div>
              {#if distHasSku === false && distSku.trim() !== ''}
                <p class="text-xs text-amber-600 dark:text-amber-400">Choose a distributor for the SKU, or use the MPN field instead.</p>
              {/if}
              {#if distError}
                <p class="text-xs text-destructive">{distError}</p>
              {/if}
              <div class="flex justify-end">
                <Button size="sm" onclick={doDistResolve} disabled={!distCanResolve}>Look up</Button>
              </div>
            {:else if distStep === 'resolving'}
              <div class="flex items-center gap-3 py-6 text-muted-foreground">
                <div class="h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent"></div>
                <p class="text-sm">Looking up distributors…</p>
              </div>
            {:else if distStep === 'review'}
              <div class="flex items-center gap-1.5">
                <button
                  type="button"
                  class="text-muted-foreground hover:text-foreground transition-colors"
                  onclick={resetDistImport}
                  title="Back to lookup"
                >
                  <ChevronLeft class="h-4 w-4" />
                </button>
                <span class="text-sm font-medium">Pick a listing to import</span>
              </div>
              <ListingSelector
                enrichment={distEnrichment}
                {preferredDistId}
                bind:primaryListingKey={distPrimaryListingKey}
                bind:includedListings={distIncludedListings}
              />
              {#if distError}
                <p class="text-xs text-destructive">{distError}</p>
              {/if}
              <div class="flex justify-end">
                <Button size="sm" onclick={handleCommitDistImport} disabled={distCommitting || !distPrimaryListingKey}>
                  {distCommitting ? 'Importing…' : 'Import & resolve'}
                </Button>
              </div>
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
