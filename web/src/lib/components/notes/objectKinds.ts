import { Cpu, FolderKanban, GitBranch, ShoppingCart, Hammer, Box } from 'lucide-svelte';

// All lucide-svelte icons share this component type.
type LucideIcon = typeof Cpu;

// The linkable partrack object types. The kind doubles as the remark directive
// name in markdown (`:project[label]{#uuid}`), so these strings are the wire
// contract shared by editor, viewer, and backend manifest.
export type ObjectKind = 'part' | 'project' | 'revision' | 'order' | 'build' | 'storage';

export const OBJECT_KINDS: ObjectKind[] = ['part', 'project', 'revision', 'order', 'build', 'storage'];

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
