<script lang="ts">
  import { useQueryClient } from '@tanstack/svelte-query';
  import { pushToDistributor, requestQuote, getDistributorStatus, retryUnresolvedItems } from '$lib/api/orders.js';
  import { subscribeSSE } from '$lib/api/sse.js';
  import { getDigiKeyAuthURL } from '$lib/api/oauth.js';
  import { ApiError } from '$lib/api/client.js';
  import MetadataCard from '$lib/components/common/MetadataCard.svelte';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { ExternalLink, Copy } from 'lucide-svelte';
  import { getTrackingUrl } from '$lib/utils/tracking.js';
  import type { Order } from '$lib/types/orders.js';
  import LinkDigiKeyOrderDialog from './LinkDigiKeyOrderDialog.svelte';

  interface Props {
    teamId: string;
    orderId: string;
    order: Order;
    /** Current page path, used as OAuth return_to */
    returnTo?: string;
  }

  let { teamId, orderId, order, returnTo = '' }: Props = $props();

  const qc = useQueryClient();

  let pushing = $state(false);
  let quoting = $state(false);
  let checkingStatus = $state(false);
  let showLinkDialog = $state(false);
  const dk = $derived(order.integrations?.digikey ?? {});
  const unresolvedItems = $derived(dk.unresolved_items ?? []);
  const canRetry = $derived(
    unresolvedItems.length > 0 &&
    !!order.external_reference &&
    order.status !== 'cancelled'
  );

  const canPush = $derived(
    order.status !== 'cancelled' && order.status !== 'received'
  );
  const canQuote = $derived(
    order.status !== 'cancelled' && order.status !== 'received'
  );
  const canCheckStatus = $derived(
    !!order.external_reference &&
    (order.status === 'placed' || order.status === 'partially_received' || order.status === 'received')
  );
  const canLink = $derived(
    !order.external_reference &&
    (order.status === 'draft' || order.status === 'exported' || order.status === 'submitted' || order.status === 'placed')
  );

  let retrying = $state(false);
  let retryJobId = $state('');
  let retryAttempted = $state(0);
  let retryTotal = $state(0);
  let retryError = $state('');

  async function handleRetry() {
    if (retrying) return;
    retrying = true;
    retryError = '';
    retryAttempted = 0;
    retryTotal = unresolvedItems.length;
    try {
      const result = await retryUnresolvedItems(teamId, orderId);
      retryJobId = result.job_id;
    } catch (err: any) {
      retryError = err.message ?? 'Failed to start retry.';
      retrying = false;
    }
  }

  $effect(() => {
    if (!retryJobId) return;
    const unsub = subscribeSSE(teamId, retryJobId, (event) => {
      const d = event.data as any;
      if (d.total !== undefined) retryTotal = d.total;
      if (d.attempted !== undefined) retryAttempted = d.attempted;
      if (d.status === 'complete') {
        retrying = false;
        retryJobId = '';
        const resolved = d.resolved ?? 0;
        const stillUnresolved = (d.unresolved_skus ?? []).length;
        if (stillUnresolved === 0) {
          pushToast('success', `All ${resolved} item${resolved !== 1 ? 's' : ''} resolved`);
        } else if (resolved > 0) {
          pushToast('success', `${resolved} resolved, ${stillUnresolved} still unresolved`);
        } else {
          pushToast('error', `Retry complete — ${stillUnresolved} item${stillUnresolved !== 1 ? 's' : ''} still unresolved`);
        }
        qc.invalidateQueries({ queryKey: ['order', orderId] });
      } else if (d.status === 'error') {
        retrying = false;
        retryJobId = '';
        retryError = d.error ?? 'Retry failed.';
        pushToast('error', 'Retry failed', retryError);
      }
    });
    return unsub;
  });

  // Derive unique shipment groups by tracking number (fallback: invoice_id).
  const shipmentGroups = $derived.by(() => {
    const map = new Map<string, {
      key: string;
      tracking_number: string;
      invoice_id: number;
      shipped_date?: string;
      expected_delivery_date?: string;
      skus: string[];
      total_qty_shipped: number;
    }>();
    for (const item of dk.last_status_line_items ?? []) {
      for (const s of item.shipments) {
        const key = s.tracking_number || `inv-${s.invoice_id}`;
        if (!map.has(key)) {
          map.set(key, {
            key,
            tracking_number: s.tracking_number,
            invoice_id: s.invoice_id,
            shipped_date: s.shipped_date,
            expected_delivery_date: s.expected_delivery_date,
            skus: [],
            total_qty_shipped: 0,
          });
        }
        const g = map.get(key)!;
        if (!g.skus.includes(item.sku)) g.skus.push(item.sku);
        g.total_qty_shipped += s.quantity_shipped;
      }
    }
    return [...map.values()];
  });

  async function copyToClipboard(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      pushToast('success', 'Copied to clipboard');
    } catch {
      pushToast('error', 'Failed to copy');
    }
  }

  async function handleOAuthRequired() {
    try {
      const rt = returnTo || window.location.pathname;
      const { auth_url } = await getDigiKeyAuthURL(teamId, rt);
      window.location.href = auth_url;
    } catch (err: any) {
      pushToast('error', 'Failed to initiate DigiKey authorization', err.message);
    }
  }

  async function handlePush() {
    if (pushing) return;
    pushing = true;
    try {
      const result = await pushToDistributor(teamId, orderId);
      await qc.invalidateQueries({ queryKey: ['order', orderId] });
      pushToast('success', 'List created on DigiKey');
      if (result.list_url) {
        window.open(result.list_url, '_blank', 'noopener');
      }
    } catch (err: any) {
      if (err instanceof ApiError && err.code === 'OAUTH_REQUIRED') {
        await handleOAuthRequired();
      } else {
        pushToast('error', 'Failed to push to DigiKey', err.message);
      }
    } finally {
      pushing = false;
    }
  }

  async function handleQuote() {
    if (quoting) return;
    quoting = true;
    try {
      await requestQuote(teamId, orderId);
      await qc.invalidateQueries({ queryKey: ['order', orderId] });
      pushToast('success', 'Quote created on DigiKey');
    } catch (err: any) {
      if (err instanceof ApiError && err.code === 'OAUTH_REQUIRED') {
        await handleOAuthRequired();
      } else {
        pushToast('error', 'Failed to request quote', err.message);
      }
    } finally {
      quoting = false;
    }
  }

  async function handleCheckStatus() {
    if (checkingStatus) return;
    checkingStatus = true;
    try {
      await getDistributorStatus(teamId, orderId);
      await qc.invalidateQueries({ queryKey: ['order', orderId] });
      pushToast('success', 'Order status updated');
    } catch (err: any) {
      pushToast('error', 'Failed to check order status', err.message);
    } finally {
      checkingStatus = false;
    }
  }
</script>

<LinkDigiKeyOrderDialog
  {teamId}
  {orderId}
  bind:open={showLinkDialog}
  onSuccess={() => qc.invalidateQueries({ queryKey: ['order', orderId] })}
/>

<MetadataCard title="DigiKey">
  {#snippet right()}
    <div class="flex items-center gap-2 flex-wrap justify-end">
      {#if canLink}
        <Button variant="outline" size="sm" onclick={() => { showLinkDialog = true; }}>
          Link DigiKey Order
        </Button>
      {/if}
      {#if canPush}
        <Button variant="outline" size="sm" onclick={handlePush} disabled={pushing}>
          {pushing ? 'Pushing…' : dk.list_id ? 'Re-push List' : 'Push to DigiKey List'}
        </Button>
      {/if}
      {#if canQuote}
        <Button variant="outline" size="sm" onclick={handleQuote} disabled={quoting}>
          {quoting ? 'Requesting…' : dk.quote_id ? 'Re-request Quote' : 'Request Quote'}
        </Button>
      {/if}
      {#if canCheckStatus}
        <Button variant="outline" size="sm" onclick={handleCheckStatus} disabled={checkingStatus}>
          {checkingStatus ? 'Checking…' : 'Check Order Status'}
        </Button>
      {/if}
      {#if canRetry}
        <Button variant="outline" size="sm" onclick={handleRetry} disabled={retrying}>
          {retrying ? `Retrying ${retryAttempted}/${retryTotal}…` : `Retry ${unresolvedItems.length} unresolved`}
        </Button>
      {/if}
    </div>
  {/snippet}

  <div class="space-y-3 text-sm">
    {#if dk.list_id}
      <div class="flex items-center gap-2">
        <span class="text-muted-foreground w-28 shrink-0">MyList</span>
        {#if dk.list_url}
          <a
            href={dk.list_url}
            target="_blank"
            rel="noopener noreferrer"
            class="font-mono text-xs text-primary hover:underline flex items-center gap-1"
          >
            {dk.list_id}
            <ExternalLink class="h-3 w-3" />
          </a>
        {:else}
          <span class="font-mono text-xs">{dk.list_id}</span>
        {/if}
      </div>
    {/if}

    {#if dk.quote_id}
      <div class="flex items-center gap-2">
        <span class="text-muted-foreground w-28 shrink-0">Quote</span>
        <span class="font-mono text-xs">{dk.quote_id}</span>
        {#if dk.quote_name}
          <span class="text-muted-foreground text-xs">({dk.quote_name})</span>
        {/if}
      </div>

      {#if dk.quote_items && dk.quote_items.length > 0}
        <div class="rounded border border-border overflow-hidden">
          <table class="w-full text-xs">
            <thead class="bg-muted/50">
              <tr>
                <th class="text-left px-3 py-1.5 font-medium text-muted-foreground">SKU</th>
                <th class="text-right px-3 py-1.5 font-medium text-muted-foreground">Quoted Price</th>
                <th class="text-right px-3 py-1.5 font-medium text-muted-foreground">Stock</th>
                <th class="px-3 py-1.5 font-medium text-muted-foreground">Flags</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-border">
              {#each dk.quote_items as item}
                <tr class="hover:bg-muted/30">
                  <td class="px-3 py-1.5 font-mono">{item.sku}</td>
                  <td class="px-3 py-1.5 text-right tabular-nums">
                    {#if item.unit_price}
                      <PriceDisplay value={item.unit_price} decimals={4} />
                    {:else}
                      <span class="text-muted-foreground">—</span>
                    {/if}
                  </td>
                  <td class="px-3 py-1.5 text-right tabular-nums">{item.quantity_available ?? '—'}</td>
                  <td class="px-3 py-1.5">
                    <div class="flex items-center gap-1">
                      {#if item.is_obsolete}
                        <Badge variant="destructive" class="text-xs py-0 px-1">Obsolete</Badge>
                      {/if}
                      {#if item.is_discontinued}
                        <Badge variant="warning" class="text-xs py-0 px-1">Discontinued</Badge>
                      {/if}
                      {#if item.expiration_date}
                        <span class="text-muted-foreground">Exp <DateDisplay value={item.expiration_date} /></span>
                      {/if}
                    </div>
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    {/if}

    {#if dk.last_status}
      <div class="flex items-center gap-2">
        <span class="text-muted-foreground w-28 shrink-0">Order Status</span>
        <span class="font-medium">{dk.last_status}</span>
        {#if dk.last_status_detail}
          <span class="text-muted-foreground text-xs">— {dk.last_status_detail}</span>
        {/if}
        {#if dk.last_status_checked_at}
          <span class="text-muted-foreground text-xs ml-auto">
            checked <DateDisplay value={dk.last_status_checked_at} />
          </span>
        {/if}
      </div>

      {#if dk.last_status_total_price}
        <div class="flex items-center gap-2">
          <span class="text-muted-foreground w-28 shrink-0">Order Total</span>
          <span class="tabular-nums">
            {dk.last_status_total_price.toFixed(2)}
            {#if dk.last_status_currency}
              <span class="text-muted-foreground">{dk.last_status_currency}</span>
            {/if}
          </span>
        </div>
      {/if}

      {#if dk.last_status_ship_method}
        <div class="flex items-center gap-2">
          <span class="text-muted-foreground w-28 shrink-0">Ship Method</span>
          <span>{dk.last_status_ship_method}</span>
        </div>
      {/if}

      {#if dk.last_status_contact}
        {@const c = dk.last_status_contact}
        <div class="flex items-center gap-2">
          <span class="text-muted-foreground w-28 shrink-0">Contact</span>
          <span>
            {[c.first_name, c.last_name].filter(Boolean).join(' ')}
            {#if c.email}
              <span class="text-muted-foreground">— {c.email}</span>
            {/if}
          </span>
        </div>
      {/if}

      {#if dk.last_status_shipping_address}
        {@const a = dk.last_status_shipping_address}
        <div class="flex gap-2">
          <span class="text-muted-foreground w-28 shrink-0">Ship To</span>
          <div class="text-xs leading-relaxed">
            {#if a.company_name}<div>{a.company_name}</div>{/if}
            {#if a.line1}<div>{a.line1}</div>{/if}
            {#if a.line2}<div>{a.line2}</div>{/if}
            {#if a.city || a.state || a.zip_code}
              <div>{[a.city, a.state, a.zip_code].filter(Boolean).join(', ')}</div>
            {/if}
            {#if a.iso_code}<div class="text-muted-foreground">{a.iso_code}</div>{/if}
          </div>
        </div>
      {/if}

      {#if shipmentGroups.length > 0}
        <div class="space-y-1.5">
          <h3 class="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Shipments</h3>
          <div class="rounded border border-border overflow-hidden">
            <table class="w-full text-xs">
              <thead class="bg-muted/50">
                <tr>
                  <th class="text-left px-3 py-1.5 font-medium text-muted-foreground">Tracking #</th>
                  <th class="text-left px-3 py-1.5 font-medium text-muted-foreground">Shipped</th>
                  <th class="text-left px-3 py-1.5 font-medium text-muted-foreground">Expected</th>
                  <th class="text-left px-3 py-1.5 font-medium text-muted-foreground">Items</th>
                  <th class="text-right px-3 py-1.5 font-medium text-muted-foreground">Qty</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-border">
                {#each shipmentGroups as group}
                  <tr class="hover:bg-muted/30">
                    <td class="px-3 py-1.5">
                      <div class="flex items-center gap-1.5">
                        {#if group.tracking_number}
                          {@const trackingUrl = getTrackingUrl(group.tracking_number, dk.last_status_ship_method)}
                          {#if trackingUrl}
                            <a
                              href={trackingUrl}
                              target="_blank"
                              rel="noopener noreferrer"
                              class="font-mono hover:underline"
                            >{group.tracking_number}</a>
                          {:else}
                            <span class="font-mono">{group.tracking_number}</span>
                          {/if}
                          <button
                            type="button"
                            onclick={() => copyToClipboard(group.tracking_number)}
                            class="text-muted-foreground hover:text-foreground transition-colors"
                            title="Copy tracking number"
                          >
                            <Copy class="h-3 w-3" />
                          </button>
                        {:else}
                          <span class="text-muted-foreground">Invoice #{group.invoice_id}</span>
                        {/if}
                      </div>
                    </td>
                    <td class="px-3 py-1.5 text-muted-foreground">
                      {#if group.shipped_date}
                        <DateDisplay value={group.shipped_date} />
                      {:else}
                        —
                      {/if}
                    </td>
                    <td class="px-3 py-1.5 text-muted-foreground">
                      {group.expected_delivery_date || '—'}
                    </td>
                    <td class="px-3 py-1.5 text-muted-foreground">
                      {#if group.skus.length <= 3}
                        {group.skus.join(', ')}
                      {:else}
                        <span title={group.skus.join(', ')}>{group.skus.length} line items</span>
                      {/if}
                    </td>
                    <td class="px-3 py-1.5 text-right tabular-nums">{group.total_qty_shipped}</td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        </div>
      {/if}

    {/if}

    {#if unresolvedItems.length > 0}
      <div class="space-y-1.5">
        <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Unresolved Import Items</h3>
        <div class="rounded border border-amber-200 bg-amber-50 dark:border-amber-900 dark:bg-amber-950/30 overflow-hidden">
          <table class="w-full text-xs">
            <thead class="bg-amber-100/50 dark:bg-amber-900/30">
              <tr>
                <th class="text-left px-3 py-1.5 font-medium text-amber-700 dark:text-amber-400">SKU</th>
                <th class="text-left px-3 py-1.5 font-medium text-amber-700 dark:text-amber-400">MPN</th>
                <th class="text-right px-3 py-1.5 font-medium text-amber-700 dark:text-amber-400">Qty</th>
                <th class="text-right px-3 py-1.5 font-medium text-amber-700 dark:text-amber-400">Unit Price</th>
                <th class="text-left px-3 py-1.5 font-medium text-amber-700 dark:text-amber-400">Reason</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-amber-200 dark:divide-amber-900">
              {#each unresolvedItems as item}
                <tr>
                  <td class="px-3 py-1.5 font-mono text-amber-800 dark:text-amber-300">{item.sku}</td>
                  <td class="px-3 py-1.5 font-mono text-amber-700 dark:text-amber-400">{item.mpn || '—'}</td>
                  <td class="px-3 py-1.5 text-right tabular-nums text-amber-700 dark:text-amber-400">{item.quantity_ordered}</td>
                  <td class="px-3 py-1.5 text-right tabular-nums text-amber-700 dark:text-amber-400">
                    {item.unit_price != null ? `$${item.unit_price.toFixed(4)}` : '—'}
                  </td>
                  <td class="px-3 py-1.5 text-amber-600 dark:text-amber-500">
                    {#if item.failure_reason === 'enrichment_failed'}API error{:else if item.failure_reason === 'enrichment_nil'}Not in catalog{:else}Not found{/if}
                  </td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
        {#if retryError}
          <p class="text-xs text-destructive">{retryError}</p>
        {/if}
      </div>
    {/if}

    {#if !dk.list_id && !dk.quote_id && !dk.last_status && unresolvedItems.length === 0}
      <p class="text-muted-foreground text-xs">
        No DigiKey API actions taken yet. Use the buttons above to push this order to a DigiKey List or request a quote.
      </p>
    {/if}
  </div>
</MetadataCard>
