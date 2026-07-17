<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { Popover } from 'bits-ui';
  import {
    ChevronDown, ChevronUp, ClipboardList, Ellipsis, Pencil, Plus, ShoppingCart,
    ArrowDownToLine, PackageMinus, ScanBarcode, Copy, Eraser, Download, MapPin,
    Hammer, Package, FileText, Box, TrendingDown, Layers,
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
    clearCollectionItems,
  } from '$lib/api/collections.js';
  import { getCart, bulkAddCartItems } from '$lib/api/cart.js';
  import { getBuildPreview, listBuilds } from '$lib/api/builds.js';
  import { getOrder, listOrders } from '$lib/api/orders.js';
  import { getRevision } from '$lib/api/revisions.js';
  import { listInventoryByLocation } from '$lib/api/storage.js';
  import { replenishPreview } from '$lib/api/inventory.js';
  import type { PartSummary } from '$lib/types/parts.js';
  import { DropdownMenu, DropdownMenuItem, DropdownMenuSeparator } from '$lib/components/ui/dropdown-menu/index.js';
  import PartSearch from '$lib/components/parts/PartSearch.svelte';
  import ConfirmDialog from '$lib/components/common/ConfirmDialog.svelte';
  import ListPanelRow from './ListPanelRow.svelte';
  import ListCheckoutDialog from './ListCheckoutDialog.svelte';
  import PickerDialog, { type PickerRow } from './PickerDialog.svelte';
  import AssignStorageDialog from './AssignStorageDialog.svelte';

  // The floating list panel: the working surface for part collections. Mounted
  // at the app-shell level so it survives navigation; collapses to the corner
  // pill, which is the feature's only launcher (no sidebar entry).
  //
  // Verb architecture: every verb has a fixed home — list-object verbs in the
  // header ⋯ menu, source verbs ("copy in") in Add from ▾, sink verbs ("paste
  // out") in Use ▾. Page context and scanner state only decide which verbs get
  // PROMOTED to inline buttons (max 2); unavailable verbs stay visible in their
  // menu, disabled with a reason.
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
  const otherLists = $derived(collections.filter((c) => c.id !== activeId));

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
  const quantified = $derived(items.filter((i) => i.quantity != null));

  // ── Page context: pre-targets + promotion, never availability ────────────
  const path = $derived(currentRoute().path);
  const onCartPage = $derived(!!teamId && path.startsWith(`/teams/${teamId}/cart`));
  const pageBuildId = $derived(matchRoute('/teams/:teamId/builds/:buildId', path)?.buildId ?? null);
  const pageOrderId = $derived(matchRoute('/teams/:teamId/orders/:orderId', path)?.orderId ?? null);
  const pageRevision = $derived(matchRoute('/teams/:teamId/projects/:projectId/revisions/:revId', path));
  const pageLocationId = $derived(matchRoute('/teams/:teamId/storage/:locationId', path)?.locationId ?? null);
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

  // ── Header: switcher, rename, list-object menu ───────────────────────────
  let switcherOpen = $state(false);
  let menuOpen = $state(false);
  let renaming = $state(false);
  let renameValue = $state('');
  let deleteConfirmOpen = $state(false);
  let clearConfirmOpen = $state(false);

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

  async function duplicateList() {
    if (!detail || verbBusy) return;
    verbBusy = true;
    try {
      const c = await createCollection(teamId, `Copy of ${detail.name}`);
      if (items.length > 0) {
        await bulkAddCollectionItems(
          teamId, c.id,
          items.map((i) => ({ part_id: i.part_id, quantity: i.quantity, note: i.note })),
        );
      }
      setActiveList(c.id);
      invalidate();
      pushToast('success', `Duplicated to “${c.name}”`);
    } catch (err) {
      toastError(err);
    } finally {
      verbBusy = false;
    }
  }

  async function clearItems() {
    if (!activeId) return;
    try {
      const res = await clearCollectionItems(teamId, activeId);
      invalidate();
      pushToast('success', `Removed ${res.removed} item${res.removed === 1 ? '' : 's'}`);
    } catch (err) {
      toastError(err);
    }
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

  // ── Source verbs: copy parts INTO the list ────────────────────────────────
  let verbBusy = $state(false);
  let checkoutOpen = $state(false);
  let assignStorageOpen = $state(false);

  async function addFromCart() {
    if (!activeId || verbBusy) return;
    verbBusy = true;
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
      verbBusy = false;
    }
  }

  // Pull a build's resolved demand into the list — the durable version of
  // computeBuildDemand's ephemeral collection (preview honours overrides,
  // exclusions, and build quantity).
  async function addFromBuild(buildId: string) {
    if (!activeId || verbBusy) return;
    verbBusy = true;
    try {
      const preview = await getBuildPreview(teamId, buildId);
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
      verbBusy = false;
    }
  }

  async function addFromOrder(orderId: string) {
    if (!activeId || verbBusy) return;
    verbBusy = true;
    try {
      const order = await getOrder(teamId, orderId);
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
      verbBusy = false;
    }
  }

  // Resolved BOM lines at per-board quantity — "the parts in this design"
  // without creating a build.
  async function addFromRevision() {
    if (!activeId || !pageRevision || verbBusy) return;
    verbBusy = true;
    try {
      const rev = await getRevision(teamId, pageRevision.projectId, pageRevision.revId);
      const toAdd = rev.items
        .filter((i) => i.resolved_part_id && !i.ignored && i.quantity > 0)
        .map((i) => ({ part_id: i.resolved_part_id!, quantity: i.quantity }));
      if (toAdd.length === 0) {
        pushToast('info', 'This revision has no resolved line items.');
        return;
      }
      await bulkAddCollectionItems(teamId, activeId, toAdd);
      invalidate();
      pushToast('success', `Added ${toAdd.length} part${toAdd.length === 1 ? '' : 's'} from the revision`, 'Per-board quantities');
    } catch (err) {
      toastError(err);
    } finally {
      verbBusy = false;
    }
  }

  // Everything living at the open storage location, as bare membership — the
  // start of a reorganize/audit workflow (stock is a live column anyway).
  async function addFromLocation() {
    if (!activeId || !pageLocationId || verbBusy) return;
    verbBusy = true;
    try {
      const res = await listInventoryByLocation(teamId, pageLocationId, { limit: 500 });
      if (res.items.length === 0) {
        pushToast('info', 'Nothing is stored at this location.');
        return;
      }
      await bulkAddCollectionItems(teamId, activeId, res.items.map((e) => ({ part_id: e.part_id })));
      invalidate();
      pushToast(
        'success',
        `Added ${res.items.length} part${res.items.length === 1 ? '' : 's'} from this location`,
        res.total > res.items.length ? `First ${res.items.length} of ${res.total}` : undefined,
      );
    } catch (err) {
      toastError(err);
    } finally {
      verbBusy = false;
    }
  }

  // Below-minimum parts at their reorder quantity — the review-first
  // alternative to the cart's one-shot "Add low stock".
  async function addLowStock() {
    if (!activeId || verbBusy) return;
    verbBusy = true;
    try {
      const res = await replenishPreview(teamId);
      const toAdd = res.items
        .map((i) => ({ part_id: i.part_id, quantity: i.quantity_to_add_to_cart ?? i.shortfall }))
        .filter((x) => x.quantity > 0);
      if (toAdd.length === 0) {
        pushToast('info', 'No parts are below their minimum stock.');
        return;
      }
      await bulkAddCollectionItems(teamId, activeId, toAdd);
      invalidate();
      pushToast('success', `Added ${toAdd.length} low-stock part${toAdd.length === 1 ? '' : 's'}`);
    } catch (err) {
      toastError(err);
    } finally {
      verbBusy = false;
    }
  }

  async function addFromList(sourceId: string) {
    if (!activeId || verbBusy) return;
    verbBusy = true;
    try {
      const src = await getCollection(teamId, sourceId);
      if (src.items.length === 0) {
        pushToast('info', 'That list is empty.');
        return;
      }
      await bulkAddCollectionItems(
        teamId, activeId,
        src.items.map((i) => ({ part_id: i.part_id, quantity: i.quantity, note: i.note })),
      );
      invalidate();
      pushToast('success', `Added ${src.items.length} part${src.items.length === 1 ? '' : 's'} from “${src.name}”`);
    } catch (err) {
      toastError(err);
    } finally {
      verbBusy = false;
    }
  }

  // ── Off-page source pickers ───────────────────────────────────────────────
  let pickerOpen = $state(false);
  let pickerTitle = $state('');
  let pickerEmpty = $state('');
  let pickerRows = $state<PickerRow[]>([]);
  let pickerLoading = $state(false);
  let pickerAction: (id: string) => void = () => {};

  async function openPicker(
    title: string,
    emptyText: string,
    load: () => Promise<PickerRow[]>,
    action: (id: string) => void,
  ) {
    pickerTitle = title;
    pickerEmpty = emptyText;
    pickerRows = [];
    pickerAction = action;
    pickerOpen = true;
    pickerLoading = true;
    try {
      pickerRows = await load();
    } catch (err) {
      toastError(err);
      pickerOpen = false;
    } finally {
      pickerLoading = false;
    }
  }

  function openBuildPicker() {
    openPicker('Add from a build', 'No builds yet.', async () => {
      const res = await listBuilds(teamId, { limit: 50, sort_by: 'created_at', sort_dir: 'desc' });
      return res.items.map((b) => ({
        id: b.id,
        label: `${b.project_name} ${b.bom_revision_label} × ${b.quantity}`,
        sublabel: b.status,
      }));
    }, (id) => void addFromBuild(id));
  }

  function openOrderPicker() {
    openPicker('Add from an order', 'No orders yet.', async () => {
      const res = await listOrders(teamId, { limit: 50 });
      return res.items.map((o) => ({
        id: o.id,
        label: `${o.distributor_name} — ${o.purchase_order_number ?? o.external_reference ?? o.id.slice(0, 8)}`,
        sublabel: `${o.status} · ${o.line_item_count} item${o.line_item_count === 1 ? '' : 's'}`,
      }));
    }, (id) => void addFromOrder(id));
  }

  function openListPicker() {
    openPicker('Add from another list', 'No other lists.', async () =>
      otherLists.map((c) => ({
        id: c.id,
        label: c.name,
        sublabel: `${c.item_count} part${c.item_count === 1 ? '' : 's'}`,
      })), (id) => void addFromList(id));
  }

  // ── Sink verbs: apply the list ────────────────────────────────────────────

  // shortfallOnly sends qty − available instead of raw qty, skipping rows
  // stock already covers — a mini stock-check for ad-hoc piles.
  async function addToCart(shortfallOnly: boolean) {
    if (!detail || verbBusy) return;
    const withQty = quantified;
    if (withQty.length === 0) {
      pushToast('warning', 'No quantities on this list — set quantities first.');
      return;
    }
    let toAdd = withQty.map((i) => ({ part_id: i.part_id, quantity: i.quantity! }));
    let covered = 0;
    if (shortfallOnly) {
      toAdd = withQty
        .map((i) => ({ part_id: i.part_id, quantity: i.quantity! - Math.max(i.available_qty ?? 0, 0) }))
        .filter((x) => x.quantity > 0);
      covered = withQty.length - toAdd.length;
      if (toAdd.length === 0) {
        pushToast('success', 'Stock covers everything on this list — nothing to order.');
        return;
      }
    }
    verbBusy = true;
    try {
      await bulkAddCartItems(teamId, toAdd);
      qc.invalidateQueries({ queryKey: ['cart', teamId] });
      const skipped = items.length - withQty.length;
      const notes = [
        skipped > 0 ? `${skipped} skipped (no quantity)` : '',
        covered > 0 ? `${covered} covered by stock` : '',
      ].filter(Boolean).join(' · ');
      pushToast(
        'success',
        `Added ${toAdd.length} part${toAdd.length === 1 ? '' : 's'} from “${detail.name}” to the cart`,
        notes || undefined,
      );
    } catch (err) {
      toastError(err);
    } finally {
      verbBusy = false;
    }
  }

  function exportCsv() {
    if (!detail || items.length === 0) return;
    const esc = (v: unknown) => {
      const s = v == null ? '' : String(v);
      return /[",\n]/.test(s) ? `"${s.replaceAll('"', '""')}"` : s;
    };
    const lines = [['mpn', 'manufacturer', 'description', 'quantity', 'note', 'available'].join(',')];
    for (const i of items) {
      lines.push([i.mpn, i.manufacturer, i.description, i.quantity, i.note, i.available_qty].map(esc).join(','));
    }
    const blob = new Blob([lines.join('\n') + '\n'], { type: 'text/csv' });
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = `${detail.name.replace(/[^\w\- ]+/g, '').trim() || 'list'}.csv`;
    a.click();
    URL.revokeObjectURL(a.href);
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

  function handleAssignStorageComplete() {
    qc.invalidateQueries({ queryKey: ['inventory', teamId] });
    qc.invalidateQueries({ queryKey: ['storage', teamId] });
    invalidate();
  }

  // ── The verb registry: fixed homes, contextual promotion ─────────────────
  type IconComponent = typeof ShoppingCart;
  interface Verb {
    id: string;
    label: string;   // menu entry
    short: string;   // promoted inline button
    icon: IconComponent;
    scanner?: boolean;
    reason: string | null; // disabled reason; null = available
    run: () => void;
  }

  const emptyReason = $derived(items.length === 0 ? 'List is empty' : null);
  const qtyReason = $derived(
    !isPurchaser ? 'Requires the purchaser role'
      : items.length === 0 ? 'List is empty'
        : quantified.length === 0 ? 'No quantities set' : null,
  );

  const sourceVerbs: Verb[] = $derived([
    {
      id: 'add-from-cart', label: 'From the cart', short: 'Add from cart', icon: ArrowDownToLine,
      reason: !isPurchaser ? 'Requires the purchaser role' : null,
      run: () => void addFromCart(),
    },
    {
      id: 'add-from-build', label: pageBuildId ? 'From this build' : 'From a build…', short: 'Add from build', icon: Hammer,
      reason: null,
      run: () => (pageBuildId ? void addFromBuild(pageBuildId) : openBuildPicker()),
    },
    {
      id: 'add-from-order', label: pageOrderId ? 'From this order' : 'From an order…', short: 'Add from order', icon: Package,
      reason: null,
      run: () => (pageOrderId ? void addFromOrder(pageOrderId) : openOrderPicker()),
    },
    {
      id: 'add-from-revision', label: 'From this BOM revision', short: 'Add from BOM', icon: FileText,
      reason: pageRevision ? null : 'Open a BOM revision first',
      run: () => void addFromRevision(),
    },
    {
      id: 'add-from-location', label: 'From this storage location', short: 'Add from location', icon: Box,
      reason: pageLocationId ? null : 'Open a storage location first',
      run: () => void addFromLocation(),
    },
    {
      id: 'add-low-stock', label: 'Low-stock parts', short: 'Add low stock', icon: TrendingDown,
      reason: null,
      run: () => void addLowStock(),
    },
    {
      id: 'add-from-list', label: 'From another list…', short: 'Add from list', icon: Layers,
      reason: otherLists.length === 0 ? 'No other lists' : null,
      run: openListPicker,
    },
    {
      id: 'collect', label: 'Collect on phone', short: 'Collect', icon: ScanBarcode, scanner: true,
      reason: scannerConnected ? null : 'No scanner paired',
      run: () => void sendToPhone('collect'),
    },
  ]);

  const sinkVerbs: Verb[] = $derived([
    {
      id: 'add-to-cart', label: 'Add to cart', short: 'Add to cart', icon: ShoppingCart,
      reason: qtyReason,
      run: () => void addToCart(false),
    },
    {
      id: 'add-shortfall', label: 'Add shortfall to cart', short: 'Add shortfall', icon: ShoppingCart,
      reason: qtyReason,
      run: () => void addToCart(true),
    },
    {
      id: 'checkout', label: 'Check out…', short: 'Check out', icon: PackageMinus,
      reason: emptyReason,
      run: () => (checkoutOpen = true),
    },
    {
      id: 'assign-storage', label: 'Assign storage…', short: 'Assign storage', icon: MapPin,
      reason: emptyReason,
      run: () => (assignStorageOpen = true),
    },
    {
      id: 'export-csv', label: 'Export CSV', short: 'Export CSV', icon: Download,
      reason: emptyReason,
      run: exportCsv,
    },
    {
      id: 'filter', label: 'Filter on phone', short: 'Filter', icon: ScanBarcode, scanner: true,
      reason: !scannerConnected ? 'No scanner paired' : emptyReason,
      run: () => void sendToPhone('filter'),
    },
  ]);

  // Promotion budget: up to 2 inline slots — the page's natural verbs first,
  // then scanner verbs if slots remain. Promoted verbs stay in their menu too.
  const promoted = $derived.by(() => {
    const ids: string[] = [];
    if (onCartPage) ids.push('add-to-cart', 'add-from-cart');
    else if (pageBuildId) ids.push('add-from-build');
    else if (pageOrderId) ids.push('add-from-order');
    else if (pageRevision) ids.push('add-from-revision');
    else if (pageLocationId) ids.push('add-from-location');
    if (scannerConnected) {
      for (const id of ['collect', 'filter']) {
        if (ids.length >= 2) break;
        ids.push(id);
      }
    }
    const all = [...sourceVerbs, ...sinkVerbs];
    return ids
      .map((id) => all.find((v) => v.id === id))
      .filter((v): v is Verb => v !== undefined);
  });

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
            <Popover.Content class="z-50 w-48 rounded-md border border-border bg-popover p-1 shadow-md" side="top" align="end" sideOffset={6}>
              <button
                onclick={() => { menuOpen = false; duplicateList(); }}
                disabled={!activeId || verbBusy}
                class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-accent disabled:opacity-50"
              >
                <Copy class="h-3.5 w-3.5 text-muted-foreground" /> Duplicate list
              </button>
              <button
                onclick={() => { menuOpen = false; clearConfirmOpen = true; }}
                disabled={!activeId || items.length === 0}
                class="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-sm hover:bg-accent disabled:opacity-50"
              >
                <Eraser class="h-3.5 w-3.5 text-muted-foreground" /> Clear items
              </button>
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
        <!-- Action bar: the two verb menus + up to two promoted verbs -->
        <div class="flex flex-wrap items-center gap-1.5 border-b border-border bg-background/60 px-3 py-2">
          <DropdownMenu>
            {#snippet trigger()}
              <button class="flex items-center gap-1 rounded-md border border-border px-2 py-1 text-xs font-medium text-foreground hover:bg-accent/50">
                <Plus class="h-3 w-3" /> Add from <ChevronDown class="h-3 w-3 text-muted-foreground" />
              </button>
            {/snippet}
            {#each sourceVerbs as v, idx (v.id)}
              {#if idx > 0 && sourceVerbs[idx - 1].scanner !== v.scanner}
                <DropdownMenuSeparator />
              {/if}
              <DropdownMenuItem disabled={v.reason !== null || verbBusy} onSelect={v.run}>
                <v.icon class="h-3.5 w-3.5 shrink-0 {v.scanner ? 'text-warning' : 'text-muted-foreground'}" />
                <div class="flex min-w-0 flex-col">
                  <span>{v.label}</span>
                  {#if v.reason}<span class="text-[10px] text-muted-foreground">{v.reason}</span>{/if}
                </div>
              </DropdownMenuItem>
            {/each}
          </DropdownMenu>

          <DropdownMenu>
            {#snippet trigger()}
              <button class="flex items-center gap-1 rounded-md border border-border px-2 py-1 text-xs font-medium text-foreground hover:bg-accent/50">
                Use <ChevronDown class="h-3 w-3 text-muted-foreground" />
              </button>
            {/snippet}
            {#each sinkVerbs as v, idx (v.id)}
              {#if idx > 0 && sinkVerbs[idx - 1].scanner !== v.scanner}
                <DropdownMenuSeparator />
              {/if}
              <DropdownMenuItem disabled={v.reason !== null || verbBusy} onSelect={v.run}>
                <v.icon class="h-3.5 w-3.5 shrink-0 {v.scanner ? 'text-warning' : 'text-muted-foreground'}" />
                <div class="flex min-w-0 flex-col">
                  <span>{v.label}</span>
                  {#if v.reason}<span class="text-[10px] text-muted-foreground">{v.reason}</span>{/if}
                </div>
              </DropdownMenuItem>
            {/each}
          </DropdownMenu>

          {#each promoted as v (v.id)}
            <button
              onclick={v.run}
              disabled={v.reason !== null || verbBusy}
              title={v.reason ?? v.label}
              class="flex items-center gap-1 rounded-md border px-2 py-1 text-xs font-medium disabled:opacity-50
                {v.scanner
                  ? 'border-warning/40 bg-warning/5 text-warning hover:bg-warning/10'
                  : 'border-info/40 bg-info/5 text-info hover:bg-info/10'}"
            >
              <v.icon class="h-3 w-3" /> {v.short}
            </button>
          {/each}
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
              Nothing here yet — add parts below, pull them in via “Add from”, or scan them in with the phone.
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
  <AssignStorageDialog
    {teamId}
    listName={detail.name}
    partIds={items.map((i) => i.part_id)}
    bind:open={assignStorageOpen}
    oncomplete={handleAssignStorageComplete}
  />
{/if}

<PickerDialog
  bind:open={pickerOpen}
  title={pickerTitle}
  emptyText={pickerEmpty}
  rows={pickerRows}
  loading={pickerLoading}
  onpick={(id) => pickerAction(id)}
/>

<ConfirmDialog
  bind:open={clearConfirmOpen}
  title="Clear this list?"
  description="All rows are removed; the list itself stays. Parts and stock are not affected."
  confirmLabel="Clear"
  destructive
  onconfirm={clearItems}
/>

<ConfirmDialog
  bind:open={deleteConfirmOpen}
  title="Delete this list?"
  description="The list and its rows are removed permanently. Parts and stock are not affected."
  confirmLabel="Delete"
  destructive
  onconfirm={deleteActive}
/>
