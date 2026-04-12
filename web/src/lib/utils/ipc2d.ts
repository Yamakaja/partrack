/**
 * IPC-2D / ISO 15434 payload generator for partrack QR codes.
 *
 * Generated payloads use the same wire format as DigiKey and Mouser bag labels
 * and are decodable by the existing scanner lookup flow without modification.
 */

const GS = '\x1d'; // group (field) separator
const RS = '\x1e'; // record separator
const EOT = '\x04'; // end of transmission

/** Generates an IPC-2D payload for a part-level QR code (MPN + manufacturer only). */
export function generatePartQRPayload(opts: {
  mpn?: string | null;
  manufacturer?: string | null;
}): string {
  const fields: string[] = [];
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
  const fields: string[] = [];
  if (opts.mpn)          fields.push(`1P${opts.mpn}`);
  if (opts.manufacturer) fields.push(`1V${opts.manufacturer}`);
  fields.push(`XD${opts.distributorName}`);
  // DigiKey uses the 30P field; all other distributors use the generic P field.
  fields.push(opts.distributorName === 'DigiKey' ? `30P${opts.sku}` : `P${opts.sku}`);
  return `[)>${RS}06${GS}${fields.join(GS)}${EOT}`;
}
