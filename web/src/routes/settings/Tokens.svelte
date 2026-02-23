<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { listTokens, createToken, revokeToken } from '$lib/api/auth.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import CopyButton from '$lib/components/common/CopyButton.svelte';
  import ConfirmDialog from '$lib/components/common/ConfirmDialog.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Dialog } from 'bits-ui';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Plus } from 'lucide-svelte';
  import type { ApiToken } from '$lib/types/auth.js';

  const qc = useQueryClient();

  let tokensQuery = createQuery(() => ({
    queryKey: ['tokens'],
    queryFn: () => listTokens(),
  }));

  // Create token dialog
  let createOpen = $state(false);
  let newTokenName = $state('');
  let creating = $state(false);
  let newlyCreatedToken = $state<string | null>(null);

  // Confirm revoke dialog
  let confirmOpen = $state(false);
  let tokenToRevoke = $state<ApiToken | null>(null);
  let revoking = $state(false);

  function openCreate() {
    newTokenName = '';
    newlyCreatedToken = null;
    createOpen = true;
  }

  async function handleCreate(e: Event) {
    e.preventDefault();
    if (!newTokenName.trim()) return;
    creating = true;
    try {
      const result = await createToken(newTokenName.trim());
      newlyCreatedToken = result.token;
      qc.invalidateQueries({ queryKey: ['tokens'] });
      // Don't close yet — show the token value first
    } catch (err: any) {
      pushToast('error', 'Failed to create token', err.message);
    } finally {
      creating = false;
    }
  }

  function handleDoneViewing() {
    newlyCreatedToken = null;
    createOpen = false;
  }

  function promptRevoke(token: ApiToken) {
    tokenToRevoke = token;
    confirmOpen = true;
  }

  async function handleRevoke() {
    if (!tokenToRevoke) return;
    revoking = true;
    try {
      await revokeToken(tokenToRevoke.id);
      qc.invalidateQueries({ queryKey: ['tokens'] });
      pushToast('success', 'Token revoked');
    } catch (err: any) {
      pushToast('error', 'Failed to revoke token', err.message);
    } finally {
      revoking = false;
      tokenToRevoke = null;
    }
  }
</script>

<div class="flex flex-col h-full">
  <Topbar title="API Tokens">
    {#snippet actions()}
      <Button size="sm" onclick={openCreate}>
        <Plus class="h-4 w-4 mr-1" /> Create Token
      </Button>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6">
    {#if tokensQuery.isPending}
      <div class="text-sm text-muted-foreground">Loading tokens…</div>
    {:else if tokensQuery.isError}
      <div class="text-sm text-destructive">Error: {tokensQuery.error.message}</div>
    {:else if tokensQuery.data}
      {@const tokens = tokensQuery.data.items}
      {#if tokens.length === 0}
        <EmptyState
          title="No API tokens"
          description="Create a token to access the API programmatically."
        >
          {#snippet action()}
            <Button size="sm" onclick={openCreate}>
              <Plus class="h-4 w-4 mr-1" /> Create Token
            </Button>
          {/snippet}
        </EmptyState>
      {:else}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Created</TableHead>
              <TableHead>Last Used</TableHead>
              <TableHead></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each tokens as token (token.id)}
              <TableRow>
                <TableCell class="font-medium">{token.name}</TableCell>
                <TableCell>
                  <DateDisplay value={token.created_at} />
                </TableCell>
                <TableCell>
                  <DateDisplay value={token.last_used_at} />
                </TableCell>
                <TableCell>
                  <Button
                    variant="outline"
                    size="sm"
                    onclick={() => promptRevoke(token)}
                    class="text-destructive hover:text-destructive"
                  >
                    Revoke
                  </Button>
                </TableCell>
              </TableRow>
            {/each}
          </TableBody>
        </Table>
      {/if}
    {/if}
  </div>
</div>

<!-- Create Token Dialog -->
<Dialog.Root bind:open={createOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      {#if newlyCreatedToken}
        <Dialog.Title class="text-lg font-semibold mb-2">Token Created</Dialog.Title>
        <p class="text-sm text-muted-foreground mb-3">
          Copy your token now. It won't be shown again.
        </p>
        <div class="flex items-center gap-2 rounded-md border border-border bg-muted p-3">
          <code class="flex-1 text-xs font-mono break-all select-all">{newlyCreatedToken}</code>
          <CopyButton text={newlyCreatedToken} />
        </div>
        <div class="flex justify-end mt-4">
          <Button onclick={handleDoneViewing}>Done</Button>
        </div>
      {:else}
        <Dialog.Title class="text-lg font-semibold mb-4">Create API Token</Dialog.Title>
        <form onsubmit={handleCreate} class="space-y-3">
          <div class="space-y-1">
            <Label for="token-name">Token Name <span class="text-destructive">*</span></Label>
            <Input
              id="token-name"
              bind:value={newTokenName}
              placeholder="e.g. CI/CD pipeline"
              required
            />
          </div>
          <div class="flex justify-end gap-2 pt-2">
            <Button variant="outline" type="button" onclick={() => { createOpen = false; }}>Cancel</Button>
            <Button type="submit" disabled={creating || !newTokenName.trim()}>
              {creating ? 'Creating…' : 'Create Token'}
            </Button>
          </div>
        </form>
      {/if}
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<!-- Confirm Revoke Dialog -->
<ConfirmDialog
  bind:open={confirmOpen}
  title="Revoke Token"
  description={tokenToRevoke ? `Revoke token "${tokenToRevoke.name}"? This cannot be undone.` : ''}
  confirmLabel={revoking ? 'Revoking…' : 'Revoke'}
  destructive
  onconfirm={handleRevoke}
/>
