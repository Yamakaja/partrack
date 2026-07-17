<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { Popover } from 'bits-ui';
  import {
    ChevronDown, ChevronUp, ClipboardList, Ellipsis, Pencil, Plus, ShoppingCart,
    ArrowDownToLine, PackageMinus, ScanBarcode,
  } from 'lucide-svelte';
  import { request } from '$lib/api/client.js';
  import { scanSessionStore } from '$lib/stores/scanSession.svelte.js';
  import { currentRoute, matchRoute, navigate } from '$lib/router/index.svelte.js';
  import { activeTeamStore } from '$lib/stores/activeTeam.svelte.js';
  import {
    listPanelStore, loadListPanel, setListPanelOpen, setActiveList,
  } from '$lib/stores/listPanel.svelte.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import {
    listCollections, createCollection, getCollection, updateCollection, deleteCollection,
    addCollectionItem, bulkAddCollectionItems, updateCollectionItem, removeCollectionItem,
  } from '$lib/api/collections.js';
  import { getCart, bulkAddCartItems } from '$lib/api/cart.js';
  import { getBuildPreview } from '$lib/api/builds.js';
  import { getOrder } from '$lib/api/orders.js';
  import type { PartSummary } from '$lib/types/parts.js';
  import PartSearch from '$lib/components/parts/PartSearch.svelte';
  import ConfirmDialog from '$lib/components/common/ConfirmDialog.svelte';
  import ListPanelRow from './ListPanelRow.svelte';
  import ListCheckoutDialog from './ListCheckoutDialog.svelte';

  // The floating list panel: the working surface for part collections. Mounted
  // at the app-shell level so it survives navigation; collapses to the corner
  // pill, which is the feature's only launcher (no sidebar entry).
  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();

  $effect(() => {
    loadListPanel(teamId);
  });

  const open = $derived(listPanelStore.open);
  const activeId = $derived(listPanelStore.activeListId);

  const collectionsQuery = createQuery(() => ({
    queryKey: ['collections', teamId],
    queryFn: () => listCollections(teamId),
    enabled: !!teamId,
    staleTime: 30_000,
  }));
  const collections = $derived(collectionsQuery.data?.collections ?? []);
  const activeSummary = $derived(collections.find((c) => c.id === activeId));

  // Reconcile the persisted active list against reality (deleted/archived
  // elsewhere): fall back to the most recently used list, or none.
  $effect(() => {
    if (!collectionsQuery.data) return;
    const lists = collectionsQuery.data.collections;
    if (listPanelStore.activeListId && !lists.some((l) => l.id === listPanelStore.activeListId)) {
      setActiveList(lists[0]?.id ?? null);
    } else if (!listPanelStore.activeListId && lists.length > 0) {
      setActiveList(lists[0].id);
    }
  });

  const detailQuery = createQuery(() => ({
    queryKey: ['collection', teamId, activeId],
    queryFn: () => getCollection(teamId, activeId!),
    enabled: !!teamId && !!activeId && open,
  }));
  const detail = $derived(detailQuery.data);
  const items = $derived(detail?.items ?? []);
  const totalPieces = $derived(items.reduce((s, i) => s + (i.quantity ?? 0), 0));
  const unquantified = $derived(items.filter((i) => i.quantity == null).length);

  // ── Page context: which verbs the action bar offers ─────────────────────
  const path = $derived(currentRoute().path);
  const onCartPage = $derived(!!teamId && path.startsWith(`/teams/${teamId}/cart`));
  const pageBuildId = $derived(matchRoute('/teams/:teamId/builds/:buildId', path)?.buildId ?? null);
  const pageOrderId = $derived(matchRoute('/teams/:teamId/orders/:orderId', path)?.orderId ?? null);
  const role = $derived(activeTeamStore.current?.role ?? 'viewer');
  const isPurchaser = $derived(role === 'purchaser' || role === 'admin');
  const scannerConnected = $derived(scanSessionStore.current?.liveConnected ?? false);

  function invalidate() {
    qc.invalidateQueries({ queryKey: ['collections', teamId] });
    if (activeId) qc.invalidateQueries({ queryKey: ['collection', teamId, activeId] });
  }

  function toastError(err: unknown) {
    pushToast('error', err instanceof Error ? err.message : 'Something went wrong');
  }

  // ── Header: switcher, rename, menu ───────────────────────────────────────
  let switcherOpen = $state(false);
  let menuOpen = $state(false);
  let renaming = $state(false);
  let renameValue = $state('');
  let deleteConfirmOpen = $state(false);

  async function newList() {
    switcherOpen = false;
    const c = await createCollection(teamId);
    setActiveList(c.id);
    invalidate();
  }

  function startRename() {
    if (!detail) return;
    renameValue = detail.name;
    renaming = true;
  }

  async function saveRename() {
    if (!renaming || !activeId) return;
    renaming = false;
    const name = renameValue.trim();
    if (!name || name === detail?.name) return;
    await updateCollection(teamId, activeId, { name });
    invalidate();
  }

  async function archiveActive() {
    if (!activeId) return;
    menuOpen = false;
    await updateCollection(teamId, activeId, { archived: true });
    setActiveList(null);
    invalidate();
  }

  async function deleteActive() {
    if (!activeId) return;
    await deleteCollection(teamId, activeId);
    setActiveList(null);
    invalidate();
  }

  // ── Add-line: constrained part picker → qty field ────────────────────────
  let stagedPart = $state<PartSummary | null>(null);
  let stagedQty = $state('');
  let searchKey = $state(0);
  let qtyInput = $state<HTMLInputElement | null>(null);
  let adding = $state(false);

  function stagePart(part: PartSummary) {
    stagedPart = part;
    // Selection hands focus to the qty field; Enter there commits (empty = no qty).
    setTimeout(() => qtyInput?.focus(), 0);
  }

  async function commitAdd() {
    if (!stagedPart || adding) return;
    let listId = activeId;
    if (!listId) {
      const c = await createCollection(teamId);
      setActiveList(c.id);
      listId = c.id;
    }
    const trimmed = stagedQty.trim();
    const qty = trimmed === '' ? null : Number.parseInt(trimmed, 10);
    if (qty !== null && (!Number.isFinite(qty) || qty <= 0)) {
      pushToast('warning', 'Quantity must be a positive number.');
      return;
    }
    adding = true;
    try {
      await addCollectionItem(teamId, listId, { part_id: stagedPart.id, quantity: qty });
      stagedPart = null;
      stagedQty = '';
      searchKey += 1; // remount PartSearch to clear its query
      invalidate();
    } catch (err) {
      toastError(err);
    } finally {
      adding = false;
    }
  }

  function qtyKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault();
      commitAdd();
    } else if (e.key === 'Escape') {
      stagedPart = null;
      stagedQty = '';
      searchKey += 1;
    }
  }

  // ── Row mutations ─────────────────────────────────────────────────────────
  async function handleItemUpdate(itemId: string, body: { quantity?: number | null; note?: string | null }) {
    if (!activeId) return;
    await updateCollectionItem(teamId, activeId, itemId, body);
    invalidate();
  }

  async function handleItemDelete(itemId: string) {
    if (!activeId) return;
    await removeCollectionItem(teamId, activeId, itemId);
    invalidate();
  }

  // ── Verbs ─────────────────────────────────────────────────────────────────
  let checkoutOpen = $state(false);
  let cartBusy = $state(false);

  async function addToCart() {
    if (!detail || cartBusy) return;
    const withQty = items.filter((i) => i.quantity != null);
    if (withQty.length === 0) {
      pushToast('warning', 'No quantities on this list — set quantities first.');
      return;
    }
    cartBusy = true;
    try {
      await bulkAddCartItems(teamId, withQty.map((i) => ({ part_id: i.part_id, quantity: i.quantity! })));
      qc.invalidateQueries({ queryKey: ['cart', teamId] });
      const skipped = items.length - withQty.length;
      pushToast(
        'success',
        `Added ${withQty.length} part${withQty.length === 1 ? '' : 's'} from “${detail.name}” to the cart`,
        skipped > 0 ? `${skipped} skipped (no quantity)` : undefined,
      );
    } catch (err) {
      toastError(err);
    } finally {
      cartBusy = false;
    }
  }

  async function addFromCart() {
    if (!activeId || cartBusy) return;
    cartBusy = true;
    try {
      const cart = await getCart(teamId);
      if (cart.items.length === 0) {
        pushToast('info', 'The cart is empty.');
        return;
      }
      await bulkAddCollectionItems(
        teamId, activeId,
        cart.items.map((ci) => ({ part_id: ci.part_id, quantity: ci.quantity })),
      );
      invalidate();
      pushToast('success', `Added ${cart.items.length} part${cart.items.length === 1 ? '' : 's'} from the cart`);
    } catch (err) {
      toastError(err);
    } finally {
      cartBusy = false;
    }
  }

  // Pull the current build's resolved demand into the list — the durable
  // version of computeBuildDemand's ephemeral collection (preview honours
  // overrides, exclusions, and build quantity).
  async function addFromBuild() {
    if (!activeId || !pageBuildId || cartBusy) return;
    cartBusy = true;
    try {
      const preview = await getBuildPreview(teamId, pageBuildId);
      const toAdd = preview.items
        .filter((i) => i.resolved_part_id && i.effective_total_quantity > 0)
        .map((i) => ({ part_id: i.resolved_part_id!, quantity: i.effective_total_quantity }));
      if (toAdd.length === 0) {
        pushToast('info', 'This build has no resolved demand.');
        return;
      }
      await bulkAddCollectionItems(teamId, activeId, toAdd);
      invalidate();
      pushToast('success', `Added ${toAdd.length} part${toAdd.length === 1 ? '' : 's'} from the build`);
    } catch (err) {
      toastError(err);
    } finally {
      cartBusy = false;
    }
  }

  async function addFromOrder() {
    if (!activeId || !pageOrderId || cartBusy) return;
    cartBusy = true;
    try {
      const order = await getOrder(teamId, pageOrderId);
      const toAdd = order.items
        .filter((i) => i.quantity_ordered > 0)
        .map((i) => ({ part_id: i.part_id, quantity: i.quantity_ordered }));
      if (toAdd.length === 0) {
        pushToast('info', 'This order has no line items.');
        return;
      }
      await bulkAddCollectionItems(teamId, activeId, toAdd);
      invalidate();
      pushToast('success', `Added ${toAdd.length} part${toAdd.length === 1 ? '' : 's'} from the order`);
    } catch (err) {
      toastError(err);
    } finally {
      cartBusy = false;
    }
  }

  // Hand the active list to the paired phone: as a Collect target (scan bags
  // into the list) or a Filter target (scan bags against it).
  async function sendToPhone(mode: 'collect' | 'filter') {
    const sess = scanSessionStore.current;
    if (!activeId || !sess) return;
    try {
      await request('PATCH', `/api/v1/teams/${teamId}/scan-sessions/${sess.id}/context`, {
        collection_id: activeId,
        mode,
      });
      pushToast('success', mode === 'collect'
        ? 'Sent to the scanner — start scanning bags'
        : 'Filter sent to the scanner — scan the pile');
    } catch (err) {
      toastError(err);
    }
  }

  async function handleCheckoutComplete(opts: { archive: boolean }) {
    qc.invalidateQueries({ queryKey: ['checkouts', teamId] });
    qc.invalidateQueries({ queryKey: ['inventory', teamId] });
    qc.invalidateQueries({ queryKey: ['parts', teamId] });
    if (opts.archive && activeId) {
      await updateCollection(teamId, activeId, { archived: true });
      setActiveList(null);
    }
    invalidate();
  }

  function relTime(iso: string): string {
    const s = (Date.now() - new Date(iso).getTime()) / 1000;
    if (s < 90) return 'just now';
    if (s < 3600) return `${Math.round(s / 60)} min ago`;
    if (s < 86400) return `${Math.round(s / 3600)} h ago`;
    return `${Math.round(s / 86400)} d ago`;
  }
</script>

<div class="fixed bottom-4 right-4 z-40">
  {#if !open}
    <!-- Collapsed: the pill IS the launcher — always present, never fully gone. -->
    <button
      onclick={() => setListPanelOpen(true)}
      class="flex items-center gap-2 rounded-full border border-border bg-card px-4 py-2 text-sm font-medium shadow-lg hover:bg-accent/50"
    >
      <ClipboardList class="h-4 w-4 text-primary" />
      {#if activeSummary}
        <span class="max-w-40 truncate">{activeSummary.name}</span>
        <span class="tabular-nums text-xs text-muted-foreground">
          {activeSummary.item_count}{activeSummary.total_quantity > 0 ? ` · ${activeSummary.total_quantity.toLocaleString()} pcs` : ''}
        </span>
      {:else}
        <span>Lists</span>
      {/if}
      <ChevronUp class="h-3.5 w-3.5 text-muted-foreground" />
    </button>
  {:else}
    <div class="flex w-96 max-w-[calc(100vw-2rem)] flex-col rounded-xl border border-border bg-card shadow-xl" style="max-height: min(560px, calc(100vh - 4rem));">
      <!-- Header: active list name + switcher, rename, collapse -->
      <div class="flex items-center gap-1.5 border-b border-border px-3 py-2">
        <Popover.Root open={switcherOpen} onOpenChange={(v) => (switcherOpen = v)}>
          <Popover.Trigger>
            <button class="flex min-w-0 items-center gap-1.5 rounded px-1 py-0.5 text-sm font-semibold hover:bg-accent/50">
              {#if renaming}
                <!-- placeholder while renaming; input rendered below -->
              {:else}
                <span class="max-w-48 truncate">{detail?.name ?? activeSummary?.name ?? 'Lists'}</span>
                <ChevronDown class="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              {/if}
            </button>
          </Popover.Trigger>
          <Popover.Content class="z-50 w-64 rounded-md border border-border bg-popover p-1 shadow-md" side="top" align="start" sideOffset={6}>
            <div class="max-h-56 overflow-y-auto">
              {#each collections as c (c.id)}
                <button
                  onclick={() => { setActiveList(c.id); switcherOpen = false; }}
                  class="flex w-full items-center justify-between gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-accent
                    {c.id === activeId ? 'bg-accent/60 font-medium' : ''}"
                >
                  <span class="min-w-0 truncate">{c.name}</span>
                  <span class="shrink-0 text-[11px] tabular-nums text-muted-foreground">
                    {c.item_count} · {relTime(c.updated_at)}
                  </span>
                </button>
              {:else}
                <p class="px-2 py-1.5 text-xs text-muted-foreground">No lists yet.</p>
              {/each}
            </div>
            <div class="mt-1 flex items-center justify-between border-t border-border pt-1">
              <button onclick={newList} class="flex items-center gap-1 rounded px-2 py-1 text-xs text-foreground hover:bg-accent">
                <Plus class="h-3 w-3" /> New list
              </button>
              <button
                onclick={() => { switcherOpen = false; navigate(`/teams/${teamId}/lists`); }}
                class="rounded px-2 py-1 text-xs text-muted-foreground hover:bg-accent hover:text-foreground"
              >
                Manage lists…
              </button>
            </div>
          </Popover.Content>
        </Popover.Root>

        {#if renaming}
          <input
            bind:value={renameValue}
            onkeydown={(e) => {
              if (e.key === 'Enter') { e.preventDefault(); saveRename(); }
              else if (e.key === 'Escape') renaming = false;
            }}
            onblur={saveRename}
            class="min-w-0 flex-1 rounded border border-input bg-background px-1.5 py-0.5 text-sm font-semibold focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          />
        {/if}

        <div class="ml-auto flex items-center gap-0.5">
          {#if activeId && !renaming}
            <button onclick={startRename} title="Rename list" class="rounded p-1 text-muted-foreground hover:bg-accent/50 hover:text-foreground">
              <Pencil class="h-3.5 w-3.5" />
            </button>
          {/if}
          <Popover.Root open={menuOpen} onOpenChange={(v) => (menuOpen = v)}>
            <Popover.Trigger>
              <button title="List actions" class="rounded p-1 text-muted-foreground hover:bg-accent/50 hover:text-foreground">
                <Ellipsis class="h-4 w-4" />
              </button>
            </Popover.Trigger>
            <Popover.Content class="z-50 w-44 rounded-md border border-border bg-popover p-1 shadow-md" side="top" align="end" sideOffset={6}>
              <button
                onclick={archiveActive}
                disabled={!activeId}
                class="w-full rounded px-2 py-1.5 text-left text-sm hover:bg-accent disabled:opacity-50"
              >
                Archive list
              </button>
              <button
                onclick={() => { menuOpen = false; deleteConfirmOpen = true; }}
                disabled={!activeId}
                class="w-full rounded px-2 py-1.5 text-left text-sm text-destructive hover:bg-accent disabled:opacity-50"
              >
                Delete list
              </button>
            </Popover.Content>
          </Popover.Root>
          <button onclick={() => setListPanelOpen(false)} title="Collapse" class="rounded p-1 text-muted-foreground hover:bg-accent/50 hover:text-foreground">
            <ChevronDown class="h-4 w-4" />
          </button>
        </div>
      </div>

      {#if activeId}
        <!-- Action bar: page-contextual verbs left, general verbs right -->
        <div class="flex flex-wrap items-center gap-1.5 border-b border-border bg-background/60 px-3 py-2">
          {#if onCartPage && isPurchaser}
            <button
              onclick={addToCart}
              disabled={cartBusy || items.length === 0}
              class="flex items-center gap-1 rounded-md border border-info/40 bg-info/5 px-2 py-1 text-xs font-medium text-info hover:bg-info/10 disabled:opacity-50"
            >
              <ShoppingCart class="h-3 w-3" /> Add to cart
            </button>
            <button
              onclick={addFromCart}
              disabled={cartBusy}
              class="flex items-center gap-1 rounded-md border border-info/40 bg-info/5 px-2 py-1 text-xs font-medium text-info hover:bg-info/10 disabled:opacity-50"
            >
              <ArrowDownToLine class="h-3 w-3" /> Add from cart
            </button>
          {:else if pageBuildId}
            <button
              onclick={addFromBuild}
              disabled={cartBusy}
              class="flex items-center gap-1 rounded-md border border-info/40 bg-info/5 px-2 py-1 text-xs font-medium text-info hover:bg-info/10 disabled:opacity-50"
            >
              <ArrowDownToLine class="h-3 w-3" /> Add from build
            </button>
          {:else if pageOrderId}
            <button
              onclick={addFromOrder}
              disabled={cartBusy}
              class="flex items-center gap-1 rounded-md border border-info/40 bg-info/5 px-2 py-1 text-xs font-medium text-info hover:bg-info/10 disabled:opacity-50"
            >
              <ArrowDownToLine class="h-3 w-3" /> Add from order
            </button>
          {/if}
          {#if scannerConnected}
            <button
              onclick={() => sendToPhone('collect')}
              title="Scan bags into this list with the phone"
              class="flex items-center gap-1 rounded-md border border-warning/40 bg-warning/5 px-2 py-1 text-xs font-medium text-warning hover:bg-warning/10"
            >
              <ScanBarcode class="h-3 w-3" /> Collect
            </button>
            <button
              onclick={() => sendToPhone('filter')}
              disabled={items.length === 0}
              title="Scan a pile against this list — is each bag on it?"
              class="flex items-center gap-1 rounded-md border border-warning/40 bg-warning/5 px-2 py-1 text-xs font-medium text-warning hover:bg-warning/10 disabled:opacity-50"
            >
              <ScanBarcode class="h-3 w-3" /> Filter
            </button>
          {/if}
          <button
            onclick={() => (checkoutOpen = true)}
            disabled={items.length === 0}
            class="ml-auto flex items-center gap-1 rounded-md border border-border px-2 py-1 text-xs text-foreground hover:bg-accent/50 disabled:opacity-50"
          >
            <PackageMinus class="h-3 w-3" /> Check out
          </button>
        </div>

        <!-- Tally: one quiet sentence -->
        {#if items.length > 0}
          <div class="border-b border-border px-3 py-1.5 text-[11.5px] text-muted-foreground">
            <span class="font-semibold text-foreground tabular-nums">{items.length}</span>
            part{items.length === 1 ? '' : 's'}
            {#if totalPieces > 0}
              · <span class="font-semibold text-foreground tabular-nums">{totalPieces.toLocaleString()}</span> pcs
            {/if}
            {#if unquantified > 0}
              · <span class="text-warning">{unquantified} without qty</span>
            {/if}
          </div>
        {/if}

        <!-- Rows -->
        <div class="min-h-0 flex-1 overflow-y-auto">
          {#if detailQuery.isPending}
            <p class="px-3 py-4 text-xs text-muted-foreground">Loading…</p>
          {:else if items.length === 0}
            <p class="px-3 py-4 text-xs text-muted-foreground">
              Nothing here yet — add parts below, or scan them in with the phone (soon).
            </p>
          {:else}
            {#each items as item (item.id)}
              <ListPanelRow {teamId} {item} onupdate={handleItemUpdate} ondelete={handleItemDelete} />
            {/each}
          {/if}
        </div>
      {:else}
        <div class="px-3 py-6 text-center text-sm text-muted-foreground">
          <p>A list is a private scratchpad of parts.</p>
          <button onclick={newList} class="mt-2 inline-flex items-center gap-1 rounded-md border border-border px-2.5 py-1.5 text-xs font-medium hover:bg-accent/50">
            <Plus class="h-3.5 w-3.5" /> New list
          </button>
        </div>
      {/if}

      <!-- Add-line: catalog part picker → qty -->
      {#if activeId}
        <div class="border-t border-border p-2">
          <div class="flex items-center gap-2">
            <div class="min-w-0 flex-1">
              {#key searchKey}
                <PartSearch {teamId} placeholder="Add a part…" dropUp onselect={stagePart} />
              {/key}
            </div>
            <input
              bind:this={qtyInput}
              bind:value={stagedQty}
              onkeydown={qtyKeydown}
              placeholder="qty"
              inputmode="numeric"
              disabled={!stagedPart}
              class="h-9 w-16 rounded-md border border-input bg-background px-2 text-right text-sm tabular-nums placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-40"
            />
            <button
              onclick={commitAdd}
              disabled={!stagedPart || adding}
              class="flex h-9 items-center rounded-md border border-border px-2.5 text-sm hover:bg-accent/50 disabled:opacity-40"
              title="Add to list (Enter)"
            >
              <Plus class="h-4 w-4" />
            </button>
          </div>
        </div>
      {/if}
    </div>
  {/if}
</div>

{#if detail}
  <ListCheckoutDialog
    {teamId}
    listName={detail.name}
    {items}
    bind:open={checkoutOpen}
    oncomplete={handleCheckoutComplete}
  />
{/if}

<ConfirmDialog
  bind:open={deleteConfirmOpen}
  title="Delete this list?"
  description="The list and its rows are removed permanently. Parts and stock are not affected."
  confirmLabel="Delete"
  destructive
  onconfirm={deleteActive}
/>
