<script lang="ts">
  import { Badge } from '$lib/components/ui/badge/index.js';

  let {
    quantity_on_hand,
    minimum_stock_quantity,
  }: {
    quantity_on_hand: number;
    minimum_stock_quantity: number | null | undefined;
  } = $props();

  let variant = $derived((): 'success' | 'warning' | 'danger' => {
    if (minimum_stock_quantity == null) {
      return quantity_on_hand > 0 ? 'success' : 'danger';
    }
    if (quantity_on_hand >= minimum_stock_quantity) return 'success';
    if (quantity_on_hand > 0) return 'warning';
    return 'danger';
  });

  let label = $derived((): string => {
    if (minimum_stock_quantity == null) {
      return quantity_on_hand > 0 ? 'In Stock' : 'Out';
    }
    if (quantity_on_hand >= minimum_stock_quantity) return 'In Stock';
    if (quantity_on_hand > 0) return 'Low Stock';
    return 'Out of Stock';
  });
</script>

<Badge variant={variant()}>{label()}</Badge>
