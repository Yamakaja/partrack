/**
 * Ephemeral navigation state — values set here are consumed once by the
 * destination page on mount and then cleared, so they don't persist across
 * subsequent navigations to the same route.
 */

let _partsStockStatus = '';

export function setPartsStockStatus(value: string) {
  _partsStockStatus = value;
}

export function consumePartsStockStatus(): string {
  const v = _partsStockStatus;
  _partsStockStatus = '';
  return v;
}
