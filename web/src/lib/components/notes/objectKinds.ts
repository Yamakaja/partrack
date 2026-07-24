import { Cpu, FolderKanban, GitBranch, ShoppingCart, Hammer, Box } from 'lucide-svelte';
import type { ObjectKind } from '$lib/types/objectRef.js';

// All lucide-svelte icons share this component type.
type LucideIcon = typeof Cpu;

// The kind union and its wire contract live in $lib/types/objectRef (mirroring
// internal/objectref); this module adds only how a kind is *presented*.
export type { ObjectKind };
export { OBJECT_KINDS } from '$lib/types/objectRef.js';

export interface KindMeta {
  label: string;
  icon: LucideIcon;
}

export const KIND_META: Record<ObjectKind, KindMeta> = {
  part: { label: 'Part', icon: Cpu },
  project: { label: 'Project', icon: FolderKanban },
  revision: { label: 'BOM revision', icon: GitBranch },
  order: { label: 'Order', icon: ShoppingCart },
  build: { label: 'Build', icon: Hammer },
  storage: { label: 'Storage location', icon: Box },
};

export function kindIcon(kind: string): LucideIcon {
  return KIND_META[kind as ObjectKind]?.icon ?? KIND_META.part.icon;
}
