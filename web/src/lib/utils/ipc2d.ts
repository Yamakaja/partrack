/**
 * IPC-2D / ISO 15434 payload generator for partrack QR codes.
 *
 * Generated payloads use the same wire format as DigiKey and Mouser bag labels.
 * All partrack-generated codes include an XT (type) field as the first field.
 * Distributor codes never emit XT, so its presence signals "partrack-generated".
 *
 * Field identifiers:
 *   XT  – partrack code type: "XTpart", "XTlisting", "XTlocation"
 *   XL  – storage location UUID
 *   XC  – flat storage coordinate (comma-separated integers)
 *   XD  – explicit distributor name
 */

import type { PartLocation } from '$lib/components/mobile-scanner/types';

const GS = '\x1d'; // group (field) separator
const RS = '\x1e'; // record separator
const EOT = '\x04'; // end of transmission

/** Generates an IPC-2D payload for a part-level QR code (MPN + manufacturer only). */
export function generatePartQRPayload(opts: {
  mpn?: string | null;
  manufacturer?: string | null;
}): string {
  const fields: string[] = ['XTpart'];
  if (opts.mpn)          fields.push(`1P${opts.mpn}`);
  if (opts.manufacturer) fields.push(`1V${opts.manufacturer}`);
  return `[)>${RS}06${GS}${fields.join(GS)}${EOT}`;
}

/** Generates an IPC-2D payload for a listing-level QR code (MPN + manufacturer + distributor + SKU). */
export function generateListingQRPayload(opts: {
  mpn?: string | null;
  manufacturer?: string | null;
  distributorName: string;
  sku: string;
}): string {
  const fields: string[] = ['XTlisting'];
  if (opts.mpn)          fields.push(`1P${opts.mpn}`);
  if (opts.manufacturer) fields.push(`1V${opts.manufacturer}`);
  fields.push(`XD${opts.distributorName}`);
  // DigiKey uses the 30P field; all other distributors use the generic P field.
  fields.push(opts.distributorName === 'DigiKey' ? `30P${opts.sku}` : `P${opts.sku}`);
  return `[)>${RS}06${GS}${fields.join(GS)}${EOT}`;
}

/** Parses an IPC-2D payload that may contain a partrack storage location QR code.
 *  Returns a PartLocation if the payload is a valid XTlocation code, null otherwise.
 */
export function parseStorageLocationQR(text: string): PartLocation | null {
  const header = `[)>${RS}06${GS}`;
  if (!text.startsWith(header)) return null;
  // Strip header (and optional trailing EOT)
  const body = text.slice(header.length).replace(/\x04$/, '');
  const fields = body.split(GS);
  if (fields[0] !== 'XTlocation') return null;

  let locationId: string | null = null;
  let coordinate: number[] | null = null;

  for (const field of fields) {
    if (field.startsWith('XL')) {
      locationId = field.slice(2);
    } else if (field.startsWith('XC')) {
      const parts = field.slice(2).split(',').map(Number);
      coordinate = parts.every(n => !isNaN(n)) ? parts : null;
    }
  }

  if (!locationId) return null;
  return { id: locationId, coordinate };
}

/** Generates an IPC-2D payload for a storage location QR code. */
export function generateStorageLocationQRPayload(opts: {
  locationId: string;
  coordinate?: number[] | null;
}): string {
  const fields: string[] = ['XTlocation', `XL${opts.locationId}`];
  if (opts.coordinate && opts.coordinate.length > 0) {
    fields.push(`XC${opts.coordinate.join(',')}`);
  }
  return `[)>${RS}06${GS}${fields.join(GS)}${EOT}`;
}
