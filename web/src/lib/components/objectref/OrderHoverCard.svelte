<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import { ShoppingCart } from 'lucide-svelte';
  import { getOrder } from '$lib/api/orders.js';
  import { linkAction } from '$lib/router/index.svelte.js';
  import { statusMeta, orderLabel, TONE_BADGE, TONE_BG } from '$lib/objectref/registry.js';
  import Badge from '$lib/components/ui/badge/Badge.svelte';
  import CardShell from './CardShell.svelte';

  // The floating content of an order reference. Must render inside a
  // LinkPreview.Root; `open` mirrors the root so nothing is fetched until it
  // opens. Shares the detail page's query key, so the card and the page warm
  // each other and SSE invalidation reaches both.
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
    queryKey: ['order', id],
    queryFn: () => getOrder(teamId, id),
    enabled: open,
    staleTime: 60_000,
  }));

  const order = $derived(query.data);

  const title = $derived(
    order
      ? orderLabel(order.distributor_name, order.purchase_order_number, order.external_reference, id)
      : '',
  );

  const status = $derived(statusMeta('order', order?.status));

  // Receipt progress: how much of what was ordered has actually arrived. This is
  // the band that earns the hover — it is what you would open the order to find.
  const receipt = $derived.by(() => {
    const items = order?.items ?? [];
    if (!items.length) return null;
    const done = items.filter((i) => i.quantity_received >= i.quantity_ordered).length;
    const ordered = items.reduce((n, i) => n + i.quantity_ordered, 0);
    const received = items.reduce((n, i) => n + i.quantity_received, 0);
    return { done, total: items.length, pct: ordered > 0 ? Math.min(100, (received / ordered) * 100) : 0 };
  });

  const total = $derived.by(() => {
    const items = order?.items ?? [];
    if (!items.length) return null;
    const sum = items.reduce(
      (n, i) => n + Number(i.unit_price_snapshot) * i.quantity_ordered + Number(i.fixed_order_cost_snapshot ?? 0),
      0,
    );
    return Number.isFinite(sum) && sum > 0 ? sum : null;
  });

  function shortDate(iso: string | null | undefined): string {
    if (!iso) return '—';
    return new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
  }
</script>

<CardShell pending={query.isPending} error={query.isError} errorText="Couldn't load order details.">
  {#if order}
    {@const lines = order.items?.length ?? 0}
    <!-- Identity -->
    <div class="flex gap-3 p-3.5">
      <div class="flex h-14 w-14 shrink-0 items-center justify-center rounded-md border border-border bg-muted">
        <ShoppingCart class="h-6 w-6 text-muted-foreground/50" />
      </div>
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2">
          <span class="font-mono text-[13px] font-semibold">{title}</span>
          {#if status}
            <Badge variant={TONE_BADGE[status.tone] as never}>{status.label}</Badge>
          {/if}
        </div>
        <p class="truncate text-[11.5px] text-muted-foreground">
          {lines} line{lines === 1 ? '' : 's'}{#if total} · {total.toLocaleString(undefined, { style: 'currency', currency: 'USD' })}{/if}
        </p>
      </div>
    </div>

    <!-- Facts -->
    <div class="mx-3.5 grid grid-cols-2 gap-x-4 gap-y-2 border-t border-border py-2.5">
      <div class="min-w-0">
        <p class="text-[10px] uppercase tracking-wider text-muted-foreground">Ordered</p>
        <p class="truncate font-mono text-[11.5px] tabular-nums">{shortDate(order.created_at)}</p>
      </div>
      <div class="min-w-0">
        <p class="text-[10px] uppercase tracking-wider text-muted-foreground">By</p>
        <p class="truncate text-[11.5px]">{order.created_by_display_name ?? '—'}</p>
      </div>
      {#if order.external_reference}
        <div class="min-w-0">
          <p class="text-[10px] uppercase tracking-wider text-muted-foreground">Distributor ref</p>
          <p class="truncate font-mono text-[11.5px]">{order.external_reference}</p>
        </div>
      {/if}
      {#if order.received_at}
        <div class="min-w-0">
          <p class="text-[10px] uppercase tracking-wider text-muted-foreground">Received</p>
          <p class="truncate font-mono text-[11.5px] tabular-nums">{shortDate(order.received_at)}</p>
        </div>
      {/if}
    </div>

    <!-- Standing: receipt progress -->
    {#if receipt}
      <div class="border-t border-border px-3.5 py-2">
        <div class="flex items-center gap-2 text-[11.5px]">
          <span
            class="h-[7px] w-[7px] shrink-0 rounded-full {receipt.done === receipt.total
              ? TONE_BG.success
              : receipt.done > 0
                ? TONE_BG.warning
                : TONE_BG.muted}"
          ></span>
          <span>
            <span class="font-mono font-semibold tabular-nums">{receipt.done}</span>
            of
            <span class="font-mono font-semibold tabular-nums">{receipt.total}</span>
            line{receipt.total === 1 ? '' : 's'} received
          </span>
        </div>
        <div class="mt-1.5 h-[5px] overflow-hidden rounded-full bg-muted">
          <div class="h-full {TONE_BG.success}" style="width: {receipt.pct}%"></div>
        </div>
      </div>
    {/if}

    <!-- Actions -->
    <div class="flex items-center gap-3.5 border-t border-border px-3.5 py-2 text-[11.5px] font-medium">
      <a
        use:linkAction
        href="/teams/{teamId}/orders/{id}"
        class="ml-auto text-muted-foreground hover:text-foreground"
      >
        Open order →
      </a>
    </div>
  {/if}
</CardShell>
