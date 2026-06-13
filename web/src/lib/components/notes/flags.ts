import { Ban, CircleCheck, Star } from 'lucide-svelte';
import type { PartFlag } from '$lib/api/notes.js';

export const FLAG_OPTIONS: PartFlag[] = ['none', 'preferred', 'approved', 'banned'];

type LucideIcon = typeof Ban;

export interface FlagMeta {
  label: string;
  badgeClass: string;
  icon?: LucideIcon;
}

export const FLAG_META: Record<PartFlag, FlagMeta> = {
  none: { label: 'No flag', badgeClass: 'border-border text-muted-foreground' },
  preferred: {
    label: 'Preferred',
    badgeClass: 'border-amber-500/30 bg-amber-500/10 text-amber-600 dark:text-amber-400',
    icon: Star,
  },
  approved: {
    label: 'Approved',
    badgeClass: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
    icon: CircleCheck,
  },
  banned: {
    label: 'Banned',
    badgeClass: 'border-destructive/30 bg-destructive/10 text-destructive',
    icon: Ban,
  },
};
