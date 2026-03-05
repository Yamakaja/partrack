<script lang="ts">
  import { formatPrice } from '$lib/utils/price.js';

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

  let formatted = $derived(() => formatPrice(value, currency, decimals));
</script>

<span class={className}>{formatted()}</span>
