<script lang="ts">
  import { useQueryClient } from '@tanstack/svelte-query';
  import { removeCartItem } from '$lib/api/cart.js';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import PartCell from '$lib/components/parts/PartCell.svelte';
  import { Trash2 } from 'lucide-svelte';
  import type { CartItem } from '$lib/types/orders.js';

  let { teamId, items }: {
    teamId: string;
    items: CartItem[];
  } = $props();

  const qc = useQueryClient();
  let removingItems = $state<Set<string>>(new Set());

  async function handleRemove(itemId: string) {
    removingItems = new Set([...removingItems, itemId]);
    try {
      await removeCartItem(teamId, itemId);
      qc.invalidateQueries({ queryKey: ['cart', teamId] });
    } catch (err: any) {
      pushToast('error', 'Failed to remove item', err.message);
    } finally {
      const s = new Set(removingItems);
      s.delete(itemId);
      removingItems = s;
    }
  }
</script>

<Table>
  <TableHeader>
    <TableRow>
      <TableHead>Part MPN</TableHead>
      <TableHead>Qty</TableHead>
      <TableHead></TableHead>
    </TableRow>
  </TableHeader>
  <TableBody>
    {#each items as item (item.id)}
      <TableRow>
        <TableCell>
          <PartCell
            partId={item.part_id}
            mpn={item.part_mpn}
            {teamId}
            manufacturer={item.part_manufacturer}
            description={item.part_short_description ?? item.part_description}
          />
        </TableCell>
        <TableCell class="tabular-nums">{item.quantity}</TableCell>
        <TableCell>
          <Button
            variant="ghost"
            size="sm"
            onclick={() => handleRemove(item.id)}
            disabled={removingItems.has(item.id)}
            class="text-destructive hover:text-destructive"
          >
            <Trash2 class="h-4 w-4" />
          </Button>
        </TableCell>
      </TableRow>
    {/each}
  </TableBody>
</Table>
