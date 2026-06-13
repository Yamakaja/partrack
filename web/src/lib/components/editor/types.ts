import type { ObjectKind } from '$lib/components/notes/objectKinds.js';

export interface SlashActions {
  insertObject: (kind: ObjectKind, id: string, label: string) => void;
  insertImageFile: (f: File) => void;
  close: () => void;
}
