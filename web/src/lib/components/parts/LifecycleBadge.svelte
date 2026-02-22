<script lang="ts">
  import { Badge } from '$lib/components/ui/badge/index.js';

  let { status }: { status: string | null | undefined } = $props();

  type Config = { label: string; variant: 'default' | 'secondary' | 'destructive' | 'outline' | 'success' | 'warning' };
  const MAP: Record<string, Config> = {
    active:              { label: 'Active',      variant: 'success' },
    nrnd:                { label: 'NRND',        variant: 'warning' },
    obsolete:            { label: 'Obsolete',    variant: 'destructive' },
    deprecated:          { label: 'Deprecated',  variant: 'secondary' },
    no_longer_available: { label: 'N/A',         variant: 'outline' },
  };
  let cfg = $derived(status ? (MAP[status] ?? { label: status, variant: 'outline' as const }) : null);
</script>

{#if cfg}
  <Badge variant={cfg.variant}>{cfg.label}</Badge>
{/if}
