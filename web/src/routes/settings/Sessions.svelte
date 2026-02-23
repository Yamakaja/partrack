<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { listSessions, revokeSession } from '$lib/api/auth.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { pushToast } from '$lib/stores/toasts.svelte.js';

  const qc = useQueryClient();

  let sessionsQuery = createQuery(() => ({
    queryKey: ['sessions'],
    queryFn: () => listSessions(),
  }));

  let revokingIds = $state<Set<string>>(new Set());

  async function handleRevoke(sessionId: string) {
    revokingIds = new Set([...revokingIds, sessionId]);
    try {
      await revokeSession(sessionId);
      qc.invalidateQueries({ queryKey: ['sessions'] });
      pushToast('success', 'Session revoked');
    } catch (err: any) {
      pushToast('error', 'Failed to revoke session', err.message);
    } finally {
      const s = new Set(revokingIds);
      s.delete(sessionId);
      revokingIds = s;
    }
  }

  function truncateUA(ua: string | null): string {
    if (!ua) return '—';
    return ua.length > 60 ? ua.slice(0, 60) + '…' : ua;
  }
</script>

<div class="flex flex-col h-full">
  <Topbar title="Sessions" />

  <div class="flex-1 overflow-auto p-6">
    {#if sessionsQuery.isPending}
      <div class="text-sm text-muted-foreground">Loading sessions…</div>
    {:else if sessionsQuery.isError}
      <div class="text-sm text-destructive">Error: {sessionsQuery.error.message}</div>
    {:else if sessionsQuery.data}
      {@const sessions = sessionsQuery.data.items}
      {#if sessions.length === 0}
        <EmptyState title="No active sessions" description="You have no active sessions." />
      {:else}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Created</TableHead>
              <TableHead>Last Used</TableHead>
              <TableHead>User Agent</TableHead>
              <TableHead>Status</TableHead>
              <TableHead></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each sessions as session (session.id)}
              <TableRow>
                <TableCell>
                  <DateDisplay value={session.created_at} />
                </TableCell>
                <TableCell>
                  <DateDisplay value={session.last_seen_at} />
                </TableCell>
                <TableCell class="text-muted-foreground text-xs max-w-xs truncate" title={session.user_agent ?? ''}>
                  {truncateUA(session.user_agent)}
                </TableCell>
                <TableCell>
                  {#if session.is_current}
                    <Badge variant="success">Current</Badge>
                  {/if}
                </TableCell>
                <TableCell>
                  {#if !session.is_current}
                    <Button
                      variant="outline"
                      size="sm"
                      onclick={() => handleRevoke(session.id)}
                      disabled={revokingIds.has(session.id)}
                    >
                      {revokingIds.has(session.id) ? 'Revoking…' : 'Revoke'}
                    </Button>
                  {/if}
                </TableCell>
              </TableRow>
            {/each}
          </TableBody>
        </Table>
      {/if}
    {/if}
  </div>
</div>
