<script lang="ts">
  let {
    value,
    currency = 'EUR',
    decimals,
    class: className = '',
  }: {
    value: string | null | undefined;
    currency?: string;
    decimals?: number;
    class?: string;
  } = $props();

  let formatted = $derived(() => {
    if (!value) return '—';
    const n = parseFloat(value);
    if (isNaN(n)) return '—';
    return new Intl.NumberFormat('en-US', {
      style: 'currency',
      currency,
      minimumFractionDigits: decimals ?? 2,
      maximumFractionDigits: decimals ?? 2,
    }).format(n);
  });
</script>

<span class={className}>{formatted()}</span>
