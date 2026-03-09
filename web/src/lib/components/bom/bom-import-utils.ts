/**
 * Pure utilities for the BOM import wizard.
 *
 * Extracted from BOMImportDialog.svelte so they can be shared across step
 * components and tested independently of any Svelte reactive context.
 */

export type ColumnRole =
  | 'ignore'
  | 'designators'
  | 'quantity'
  | 'mpn'
  | 'description'
  | 'manufacturer'
  | 'name'
  | 'sku'
  | 'distributor';

/** Map of lowercased CSV header names to their auto-detected ColumnRole. */
export const AUTO_MAP: Record<string, ColumnRole> = {
  ref: 'designators',
  reference: 'designators',
  refdes: 'designators',
  designator: 'designators',
  designators: 'designators',
  refs: 'designators',
  qty: 'quantity',
  quantity: 'quantity',
  count: 'quantity',
  mpn: 'mpn',
  'manufacturer part number': 'mpn',
  'manufacturer part number 1': 'mpn',
  'part number': 'mpn',
  partnumber: 'mpn',
  part_number: 'mpn',
  description: 'description',
  desc: 'description',
  value: 'description',
  manufacturer: 'manufacturer',
  'manufacturer 1': 'manufacturer',
  mfr: 'manufacturer',
  mfg: 'manufacturer',
  'manufacturer name': 'manufacturer',
  name: 'name',
  sku: 'sku',
  'distributor sku': 'sku',
  'supplier sku': 'sku',
  distributor: 'distributor',
  'distributor name': 'distributor',
  vendor: 'distributor',
  supplier: 'distributor',
  'supplier 1': 'distributor',
  'supplier part number 1': 'sku',
};

/** Human-readable label for each ColumnRole, shown in the mapping select. */
export const ROLE_LABELS: Record<ColumnRole, string> = {
  ignore: '— Ignore —',
  designators: 'Designators *',
  quantity: 'Quantity *',
  mpn: 'MPN',
  description: 'Description',
  manufacturer: 'Manufacturer',
  name: 'Name',
  sku: 'SKU',
  distributor: 'Distributor',
};

/** Maximum display width (characters) before a cell value is truncated. */
export const COLUMN_WIDTH = 80;

/** Distributor name to distributor ID mapping collected in Step 3. */
export type DistMapping = Record<string, string | undefined>;

/** Truncate a cell value for preview display. */
export function truncateRow(s: string | undefined): string {
  if (!s) return '';
  if (s.length <= COLUMN_WIDTH) return s;
  return s.substring(0, COLUMN_WIDTH) + '...';
}

/**
 * Returns true if any non-ignore role is assigned to more than one column.
 * Used to surface a validation warning in the column-mapping step.
 */
export function checkForDuplicates(roles: ColumnRole[]): boolean {
  const seen: Record<string, boolean> = {};
  for (const role of roles) {
    if (role === 'ignore') continue;
    if (seen[role]) return true;
    seen[role] = true;
  }
  return false;
}

export interface BOMImportItem {
  reference_designators: string;
  quantity: number;
  raw_description?: string;
  mpn?: string;
  manufacturer?: string;
  name?: string;
  sku?: string;
  distributor_name?: string;
  resolved_distributor_id?: string | null;
}

/**
 * Transform parsed CSV data rows into BOM import items using the current
 * column-role assignments and an optional distributor ID mapping.
 */
export function buildItems(
  dataRows: string[][],
  columnRoles: ColumnRole[],
  mapping?: DistMapping,
): BOMImportItem[] {
  const desigIdx = columnRoles.indexOf('designators');
  const qtyIdx   = columnRoles.indexOf('quantity');
  const descIdx  = columnRoles.indexOf('description');
  const mfrIdx   = columnRoles.indexOf('manufacturer');
  const mpnIdx   = columnRoles.indexOf('mpn');
  const nameIdx  = columnRoles.indexOf('name');
  const skuIdx   = columnRoles.indexOf('sku');
  const distIdx  = columnRoles.indexOf('distributor');

  return dataRows
    .map((row) => {
      const distributorName =
        distIdx >= 0 && row[distIdx]?.trim() ? row[distIdx].trim() : undefined;
      const resolvedDistributorId =
        distributorName && mapping ? mapping[distributorName] || null : null;

      return {
        reference_designators: row[desigIdx]?.trim() ?? '',
        quantity: parseInt(row[qtyIdx] ?? '0', 10),
        raw_description:
          descIdx >= 0 && row[descIdx]?.trim() ? row[descIdx].trim() : undefined,
        mpn:
          mpnIdx >= 0 && row[mpnIdx]?.trim() ? row[mpnIdx].trim() : undefined,
        manufacturer:
          mfrIdx >= 0 && row[mfrIdx]?.trim() ? row[mfrIdx].trim() : undefined,
        name:
          nameIdx >= 0 && row[nameIdx]?.trim() ? row[nameIdx].trim() : undefined,
        sku:
          skuIdx >= 0 && row[skuIdx]?.trim() ? row[skuIdx].trim() : undefined,
        distributor_name: distributorName,
        resolved_distributor_id: resolvedDistributorId,
      };
    })
    .filter((it) => it.reference_designators && it.quantity > 0);
}
