import type { PartSummary } from '$lib/types/parts.js';

export interface SlashActions {
  insertPart: (p: PartSummary) => void;
  insertImageFile: (f: File) => void;
  close: () => void;
}
