<script lang="ts">
  let {
    value,
    currency = 'USD',
    class: className = '',
  }: {
    value: string | null | undefined;
    currency?: string;
    class?: string;
  } = $props();

  let formatted = $derived(() => {
    if (!value) return '—';
    const n = parseFloat(value);
    if (isNaN(n)) return '—';
    return new Intl.NumberFormat('en-US', { style: 'currency', currency }).format(n);
  });
</script>

<span class={className}>{formatted()}</span>
