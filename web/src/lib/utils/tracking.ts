import { getTrackingUrl as libGetTrackingUrl } from 'tracking-number-validation';

/** Returns a carrier tracking URL, or null if the carrier can't be determined. */
export function getTrackingUrl(trackingNumber: string, shipMethod?: string): string | null {
  const n = trackingNumber.trim();
  const url = libGetTrackingUrl(n);
  if (url) return url;
  // Fallback: infer from ship method string
  const method = (shipMethod ?? '').toLowerCase();
  const encoded = encodeURIComponent(n);
  if (method.includes('fedex')) return `https://www.fedex.com/fedex/track/?tracknumbers=${encoded}`;
  if (method.includes('ups')) return `https://www.ups.com/track?tracknum=${encoded}`;
  if (method.includes('usps') || method.includes('postal')) return `https://tools.usps.com/go/TrackConfirmAction?tLabels=${encoded}`;
  if (method.includes('dhl')) return `https://www.dhl.com/en/express/tracking.html?AWB=${encoded}`;
  return null;
}
