<script lang="ts">
  import { onMount } from 'svelte';
  import { Dialog } from 'bits-ui';
  import { Button } from '$lib/components/ui/button/index.js';
  import { subscribeSSE } from '$lib/api/sse.js';
  import { Loader2, XCircle } from 'lucide-svelte';

  let {
    teamId,
    jobId,
    onStartReview,
    onClose,
  }: {
    teamId: string;
    jobId: string;
    onStartReview: () => Promise<void>;
    onClose: () => void;
  } = $props();

  let open = $state(true);
  let resolved = $state(0);      // resolved_sku + resolved_mpn + resolved_prior_revision
  let resolvedSku = $state(0);
  let resolvedMpn = $state(0);
  let resolvedPrior = $state(0);
  let substitutesInherited = $state(0);
  let ambiguous = $state(0);
  let noMatch = $state(0);
  let total = $state(0);
  let done = $state(false);
  let error = $state('');
  let reviewing = $state(false);

  // Remove the ?import_job query param from the URL so a refresh won't re-open the modal.
  onMount(() => {
    history.replaceState(null, '', window.location.pathname);
  });

  $effect(() => {
    const unsub = subscribeSSE(teamId, jobId, (event) => {
      const d = event.data;
      if (d?.total !== undefined) total = d.total as number;
      if (d?.resolved !== undefined) resolved = d.resolved as number;
      if (d?.resolved_sku !== undefined) resolvedSku = d.resolved_sku as number;
      if (d?.resolved_mpn !== undefined) resolvedMpn = d.resolved_mpn as number;
      if (d?.resolved_prior_revision !== undefined) resolvedPrior = d.resolved_prior_revision as number;
      if (d?.substitutes_copied !== undefined) substitutesInherited = d.substitutes_copied as number;
      if (d?.ambiguous !== undefined) ambiguous = d.ambiguous as number;
      if (d?.no_match !== undefined) noMatch = d.no_match as number;
      if (d?.status === 'complete') {
        done = true;
      } else if (d?.status === 'error') {
        error = (d.error as string) ?? 'Import failed';
      }
    });
    return unsub;
  });

  let resolvedPct   = $derived(total > 0 ? (resolved   / total) * 100 : 0);
  let ambiguousPct  = $derived(total > 0 ? (ambiguous  / total) * 100 : 0);
  let noMatchPct    = $derived(total > 0 ? (noMatch    / total) * 100 : 0);

  async function handleStartReview() {
    reviewing = true;
    await onStartReview();
    open = false;
  }

  function handleClose() {
    open = false;
    onClose();
  }
</script>

<Dialog.Root bind:open onOpenChange={(v) => { if (!v) onClose(); }}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content
      class="fixed left-1/2 top-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg flex flex-col min-h-[380px]"
    >
      <Dialog.Title class="text-lg font-semibold mb-1">BOM Import</Dialog.Title>

      {#if error}
        <div class="flex items-center gap-3 py-4">
          <XCircle class="h-5 w-5 shrink-0 text-destructive" />
          <p class="text-sm text-destructive">Import failed: {error}</p>
        </div>
        <div class="flex justify-end mt-4">
          <Button onclick={handleClose}>Close</Button>
        </div>

      {:else if done}
        <p class="text-sm text-muted-foreground mb-4">Import complete.</p>
        <div class="rounded-md border border-border divide-y divide-border text-sm mb-6">
          <div class="flex items-center justify-between px-4 py-2">
            <span class="text-muted-foreground">Total items</span>
            <span class="font-medium tabular-nums">{total}</span>
          </div>
          <div class="flex items-center justify-between px-4 py-2">
            <span class="text-muted-foreground">Resolved by SKU</span>
            <span class="font-medium tabular-nums {resolvedSku > 0 ? 'text-green-600' : 'text-muted-foreground'}">{resolvedSku}</span>
          </div>
          <div class="flex items-center justify-between px-4 py-2">
            <span class="text-muted-foreground">Resolved by MPN</span>
            <span class="font-medium tabular-nums {resolvedMpn > 0 ? 'text-green-600' : 'text-muted-foreground'}">{resolvedMpn}</span>
          </div>
          <div class="flex items-center justify-between px-4 py-2">
            <span class="text-muted-foreground">From prior revision</span>
            <span class="font-medium tabular-nums {resolvedPrior > 0 ? 'text-green-600' : 'text-muted-foreground'}">{resolvedPrior}</span>
          </div>
          <div class="flex items-center justify-between px-4 py-2">
            <span class="text-muted-foreground">Substitutes inherited</span>
            <span class="font-medium tabular-nums {substitutesInherited > 0 ? 'text-green-600' : 'text-muted-foreground'}">{substitutesInherited}</span>
          </div>
          <div class="flex items-center justify-between px-4 py-2">
            <span class="text-muted-foreground">Ambiguous</span>
            <span class="font-medium tabular-nums {ambiguous > 0 ? 'text-amber-600' : 'text-muted-foreground'}">{ambiguous}</span>
          </div>
          <div class="flex items-center justify-between px-4 py-2">
            <span class="text-muted-foreground">Unresolved</span>
            <span class="font-medium tabular-nums {noMatch > 0 ? 'text-destructive' : 'text-muted-foreground'}">{noMatch}</span>
          </div>
        </div>
        <div class="flex justify-end gap-2">
          <Button variant="outline" onclick={handleClose}>Close</Button>
          {#if noMatch > 0 || ambiguous > 0}
            <Button onclick={handleStartReview} disabled={reviewing}>
              {#if reviewing}<Loader2 class="h-4 w-4 animate-spin" />{/if}
              Start Review
            </Button>
          {/if}
        </div>

      {:else}
        <div class="flex items-center gap-3 py-4">
          <Loader2 class="h-5 w-5 shrink-0 animate-spin text-muted-foreground" />
          <div class="flex-1 space-y-1.5">
            <p class="text-sm text-muted-foreground">
              Importing BOM…{#if total > 0}&nbsp;{resolved}/{total} resolved{#if ambiguous > 0}, {ambiguous} ambiguous{/if}{#if noMatch > 0}, {noMatch} no match{/if}{/if}
            </p>
            {#if total > 0}
              <div class="flex h-1.5 w-full overflow-hidden rounded-full bg-border">
                <div class="h-full bg-green-500 transition-all" style="width: {resolvedPct}%"></div>
                <div class="h-full bg-amber-500 transition-all" style="width: {ambiguousPct}%"></div>
                <div class="h-full bg-destructive transition-all" style="width: {noMatchPct}%"></div>
              </div>
            {/if}
          </div>
        </div>
        <div class="flex justify-end mt-2">
          <Button variant="outline" onclick={handleClose}>Close</Button>
        </div>
      {/if}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>
