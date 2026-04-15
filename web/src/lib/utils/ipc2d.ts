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
