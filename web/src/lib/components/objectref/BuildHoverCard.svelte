<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import { Hammer } from 'lucide-svelte';
  import { getBuild } from '$lib/api/builds.js';
  import { linkAction } from '$lib/router/index.svelte.js';
  import { statusMeta, revisionLabel, TONE_BADGE, TONE_BG } from '$lib/objectref/registry.js';
  import Badge from '$lib/components/ui/badge/Badge.svelte';
  import CardShell from './CardShell.svelte';

  // The floating content of a build reference. Shares the build detail page's
  // query key, so hovering warms navigation and SSE invalidation reaches both.
  let {
    id,
    teamId,
    open,
  }: {
    id: string;
    teamId: string;
    open: boolean;
  } = $props();

  const query = createQuery(() => ({
    queryKey: ['build', id],
    queryFn: () => getBuild(teamId, id),
    enabled: open,
    staleTime: 60_000,
  }));

  const build = $derived(query.data);
  const status = $derived(statusMeta('build', build?.status));
  const title = $derived(build ? revisionLabel(build.project_name, build.bom_revision_label) : '');

  // Standing: how much of the build's demand is actually covered. A part counts
  // as covered once reserved stock plus received stock meets what it needs —
  // the same rule the build page applies per row.
  const coverage = $derived.by(() => {
    const allocations = build?.allocations ?? [];
    if (!allocations.length) return null;
    const covered = allocations.filter(
      (a) => a.quantity_reserved + a.quantity_received >= a.quantity_needed,
    ).length;
    const needed = allocations.reduce((n, a) => n + a.quantity_needed, 0);
    const reserved = allocations.reduce((n, a) => n + Math.min(a.quantity_reserved, a.quantity_needed), 0);
    const received = allocations.reduce((n, a) => n + Math.min(a.quantity_received, a.quantity_needed), 0);
    return {
      covered,
      total: allocations.length,
      short: allocations.length - covered,
      receivedPct: needed > 0 ? (received / needed) * 100 : 0,
      reservedPct: needed > 0 ? (reserved / needed) * 100 : 0,
    };
  });

  function shortDate(iso: string | null | undefined): string {
    if (!iso) return '—';
    return new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
  }
</script>

<CardShell pending={query.isPending} error={query.isError} errorText="Couldn't load build details.">
  {#if build}
    <!-- Identity -->
    <div class="flex gap-3 p-3.5">
      <div class="flex h-14 w-14 shrink-0 items-center justify-center rounded-md border border-border bg-muted">
        <Hammer class="h-6 w-6 text-muted-foreground/50" />
      </div>
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2">
          <span class="text-[13px] font-semibold">{title}</span>
          {#if status}
            <Badge variant={TONE_BADGE[status.tone] as never}>{status.label}</Badge>
          {/if}
        </div>
        <p class="truncate text-[11.5px] text-muted-foreground">
          ×{build.quantity} board{build.quantity === 1 ? '' : 's'}
          {#if build.created_by_display_name}· {build.created_by_display_name}{/if}
        </p>
        {#if build.notes}
          <p class="mt-1.5 line-clamp-2 text-xs leading-relaxed">{build.notes}</p>
        {/if}
      </div>
    </div>

    <!-- Facts -->
    <div class="mx-3.5 grid grid-cols-2 gap-x-4 gap-y-2 border-t border-border py-2.5">
      <div class="min-w-0">
        <p class="text-[10px] uppercase tracking-wider text-muted-foreground">Quantity</p>
        <p class="truncate font-mono text-[11.5px] tabular-nums">{build.quantity}</p>
      </div>
      <div class="min-w-0">
        <p class="text-[10px] uppercase tracking-wider text-muted-foreground">Created</p>
        <p class="truncate font-mono text-[11.5px] tabular-nums">{shortDate(build.created_at)}</p>
      </div>
      {#if build.assembled_at}
        <div class="min-w-0">
          <p class="text-[10px] uppercase tracking-wider text-muted-foreground">Assembled</p>
          <p class="truncate font-mono text-[11.5px] tabular-nums">{shortDate(build.assembled_at)}</p>
        </div>
        <div class="min-w-0">
          <p class="text-[10px] uppercase tracking-wider text-muted-foreground">Built</p>
          <p class="truncate font-mono text-[11.5px] tabular-nums">{build.assembled_quantity ?? build.quantity}</p>
        </div>
      {:else if build.order_ids?.length}
        <div class="min-w-0">
          <p class="text-[10px] uppercase tracking-wider text-muted-foreground">Orders</p>
          <p class="truncate font-mono text-[11.5px] tabular-nums">{build.order_ids.length} linked</p>
        </div>
      {/if}
    </div>

    <!-- Standing: part coverage -->
    {#if coverage}
      <div class="border-t border-border px-3.5 py-2">
        <div class="flex items-center gap-2 text-[11.5px]">
          <span
            class="h-[7px] w-[7px] shrink-0 rounded-full {coverage.short === 0
              ? TONE_BG.success
              : coverage.covered > 0
                ? TONE_BG.warning
                : TONE_BG.danger}"
          ></span>
          <span>
            <span class="font-mono font-semibold tabular-nums">{coverage.covered}</span>
            of
            <span class="font-mono font-semibold tabular-nums">{coverage.total}</span>
            parts covered
          </span>
          {#if coverage.short > 0}
            <span class="ml-auto text-muted-foreground">{coverage.short} short</span>
          {:else if build.kitting_session}
            <span class="ml-auto"><Badge variant="info">Kitting</Badge></span>
          {/if}
        </div>
        <!-- Received stock is in hand; reserved is promised but not yet arrived. -->
        <div class="mt-1.5 flex h-[5px] overflow-hidden rounded-full bg-muted">
          <span class="h-full {TONE_BG.success}" style="width: {coverage.receivedPct}%"></span>
          <span class="h-full {TONE_BG.reserved}" style="width: {coverage.reservedPct}%"></span>
        </div>
      </div>
    {/if}

    <!-- Actions -->
    <div class="flex items-center gap-3.5 border-t border-border px-3.5 py-2 text-[11.5px] font-medium">
      <a
        use:linkAction
        href="/teams/{teamId}/projects/{build.project_id}/revisions/{build.bom_revision_id}"
        class="text-info hover:underline"
      >
        Open revision →
      </a>
      <a use:linkAction href="/teams/{teamId}/builds/{id}" class="ml-auto text-muted-foreground hover:text-foreground">
        Open build →
      </a>
    </div>
  {/if}
</CardShell>
