import type { CartItem, PriceBreak } from '$lib/types/orders.js';

export function effectiveOrderQty(item: CartItem): number {
  return item.listing?.optimized_qty ?? Math.max(item.quantity, item.listing?.moq ?? 0);
}

export function effectiveUnitPrice(item: CartItem, orderQty: number): number {
  const listing = item.listing;
  if (!listing) return 0;
  // For resolved listings the backend already computed the price at optimal qty.
  if (!listing.chosen && listing.unit_price) {
    return parseFloat(listing.unit_price);
  }
  let price = listing.unit_price ? parseFloat(listing.unit_price) : NaN;
  for (const pb of listing.price_breaks ?? []) {
    if (orderQty >= pb.break_qty) price = parseFloat(pb.unit_price);
  }
  return isNaN(price) ? 0 : price;
}

export function effectiveUnitPriceStr(item: CartItem, orderQty: number): string | null {
  const price = effectiveUnitPrice(item, orderQty);
  return price > 0 ? price.toFixed(4) : null;
}

export function calcSubtotal(item: CartItem, orderQty: number): string | null {
  if (!item.listing?.unit_price && !item.listing?.price_breaks?.length) return null;
  const price = effectiveUnitPrice(item, orderQty);
  if (price === 0) return null;
  return (price * orderQty).toFixed(2);
}

export function itemDistributor(item: CartItem): string {
  return item.listing?.distributor_name ?? 'Unassigned';
}

export function itemSku(item: CartItem): string | null {
  return item.listing?.sku ?? null;
}

export function itemMoq(item: CartItem): number {
  return item.listing?.moq ?? 0;
}

export function totalBuildDemand(item: CartItem): number {
  return (item.build_demands ?? []).reduce((s, d) => s + d.quantity_needed, 0);
}

export function activePriceBreakQty(breaks: PriceBreak[], orderQty: number): number {
  let active = -1;
  for (const pb of breaks) {
    if (orderQty >= pb.break_qty && pb.break_qty > active) active = pb.break_qty;
  }
  return active;
}

export function itemLastRefreshed(item: CartItem): string | null {
  const ts = item.listing?.last_refreshed_at;
  if (!ts) return null;
  const diff = Date.now() - new Date(ts).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs}h ago`;
  return `${Math.floor(hrs / 24)}d ago`;
}
