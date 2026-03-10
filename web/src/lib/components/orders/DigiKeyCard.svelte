<script lang="ts">
  import { useQueryClient } from '@tanstack/svelte-query';
  import { pushToDistributor, requestQuote, getDistributorStatus } from '$lib/api/orders.js';
  import { getDigiKeyAuthURL } from '$lib/api/oauth.js';
  import { ApiError } from '$lib/api/client.js';
  import MetadataCard from '$lib/components/common/MetadataCard.svelte';
  import PriceDisplay from '$lib/components/common/PriceDisplay.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { ExternalLink } from 'lucide-svelte';
  import type { Order } from '$lib/types/orders.js';

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

  const dk = $derived(order.integrations?.digikey ?? {});

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

<MetadataCard title="DigiKey">
  {#snippet right()}
    <div class="flex items-center gap-2 flex-wrap justify-end">
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

      {#if dk.last_status}
        <!-- per-line shipment status if available from latest check -->
      {/if}
    {/if}

    {#if !dk.list_id && !dk.quote_id && !dk.last_status}
      <p class="text-muted-foreground text-xs">
        No DigiKey API actions taken yet. Use the buttons above to push this order to a DigiKey List or request a quote.
      </p>
    {/if}
  </div>
</MetadataCard>
