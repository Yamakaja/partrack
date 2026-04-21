<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import {
    listBulkScanSessions, deleteBulkScanSession,
    type BulkScanSession, type BulkScanSessionCounts,
  } from '$lib/api/bulkScan.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import DateDisplay from '$lib/components/common/DateDisplay.svelte';
  import { navigate, linkAction } from '$lib/router/index.svelte.js';

  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();

  const sessionsQuery = createQuery(() => ({
    queryKey: ['bulk-scan-sessions', teamId],
    queryFn: () => listBulkScanSessions(teamId),
  }));

  let confirmDeleteId = $state<string | null>(null);
  let deleting = $state(false);

  async function handleDelete(sessionId: string) {
    deleting = true;
    try {
      await deleteBulkScanSession(teamId, sessionId);
      qc.invalidateQueries({ queryKey: ['bulk-scan-sessions', teamId] });
    } finally {
      deleting = false;
      confirmDeleteId = null;
    }
  }

  function countActionable(counts: BulkScanSessionCounts): number {
    return counts.pending_enrichment + counts.enriched + counts.error;
  }

  function countTotal(counts: BulkScanSessionCounts): number {
    return counts.pending_enrichment + counts.enriched + counts.error + counts.reviewed + counts.dismissed;
  }

  const sessions = $derived(sessionsQuery.data?.sessions ?? []);
  // Hide sessions with no actionable items by default
  let showCompleted = $state(false);
  const visible = $derived(
    showCompleted ? sessions : sessions.filter(s => countActionable(s.counts) > 0)
  );
</script>

<div class="flex flex-col h-full">
  <Topbar title="Bulk Scan Sessions" />

  <div class="flex-1 overflow-auto p-6 space-y-6">
    {#if sessionsQuery.isPending}
      <p class="text-sm text-muted-foreground">Loading…</p>
    {:else if sessionsQuery.isError}
      <p class="text-sm text-destructive">Failed to load sessions.</p>
    {:else}
      <div class="flex items-center justify-between">
        <p class="text-sm text-muted-foreground">
          {visible.length} session{visible.length === 1 ? '' : 's'}
          {#if !showCompleted && sessions.length > visible.length}
            &mdash; {sessions.length - visible.length} completed hidden
          {/if}
        </p>
        {#if sessions.some(s => countActionable(s.counts) === 0)}
          <button
            onclick={() => { showCompleted = !showCompleted; }}
            class="text-xs text-muted-foreground hover:text-foreground underline"
          >
            {showCompleted ? 'Hide completed' : 'Show completed'}
          </button>
        {/if}
      </div>

      {#if visible.length === 0}
        <div class="rounded-lg border border-border p-8 text-center">
          <p class="text-sm text-muted-foreground">No bulk scan sessions yet.</p>
          <p class="mt-1 text-xs text-muted-foreground">Enable bulk mode on the phone scanner to start scanning.</p>
        </div>
      {:else}
        <div class="space-y-3">
          {#each visible as session (session.id)}
            {@const actionable = countActionable(session.counts)}
            {@const total = countTotal(session.counts)}
            <div class="rounded-lg border border-border bg-card p-4">
              <div class="flex items-start justify-between gap-4">
                <div class="flex-1 min-w-0">
                  <div class="flex items-center gap-2 flex-wrap">
                    <span class="text-sm font-medium">
                      <DateDisplay value={session.created_at} />
                    </span>
                    {#if session.closed_at}
                      <span class="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">Closed</span>
                    {:else}
                      <span class="rounded-full bg-green-100 dark:bg-green-900/30 px-2 py-0.5 text-xs text-green-700 dark:text-green-400">Active</span>
                    {/if}
                  </div>

                  <!-- Count breakdown -->
                  <div class="mt-2 flex flex-wrap gap-3 text-xs text-muted-foreground">
                    {#if session.counts.pending_enrichment > 0}
                      <span class="flex items-center gap-1">
                        <span class="h-1.5 w-1.5 rounded-full bg-yellow-500"></span>
                        {session.counts.pending_enrichment} pending
                      </span>
                    {/if}
                    {#if session.counts.enriched > 0}
                      <span class="flex items-center gap-1">
                        <span class="h-1.5 w-1.5 rounded-full bg-blue-500"></span>
                        {session.counts.enriched} ready
                      </span>
                    {/if}
                    {#if session.counts.error > 0}
                      <span class="flex items-center gap-1">
                        <span class="h-1.5 w-1.5 rounded-full bg-red-500"></span>
                        {session.counts.error} error{session.counts.error === 1 ? '' : 's'}
                      </span>
                    {/if}
                    {#if session.counts.reviewed > 0}
                      <span class="flex items-center gap-1">
                        <span class="h-1.5 w-1.5 rounded-full bg-green-500"></span>
                        {session.counts.reviewed} done
                      </span>
                    {/if}
                    {#if session.counts.dismissed > 0}
                      <span class="text-muted-foreground/60">{session.counts.dismissed} dismissed</span>
                    {/if}
                    {#if total === 0}
                      <span class="text-muted-foreground/60">No items</span>
                    {/if}
                  </div>
                </div>

                <div class="flex items-center gap-2 shrink-0">
                  {#if actionable > 0}
                    <a
                      href={`/teams/${teamId}/bulk-scan/${session.id}`}
                      use:linkAction
                      class="rounded-md bg-primary px-3 py-1.5 text-xs font-medium text-primary-foreground hover:bg-primary/90"
                    >
                      Review →
                    </a>
                  {/if}

                  {#if confirmDeleteId === session.id}
                    <span class="text-xs text-muted-foreground">Delete?</span>
                    <button
                      onclick={() => handleDelete(session.id)}
                      disabled={deleting}
                      class="rounded-md bg-destructive px-2 py-1 text-xs font-medium text-destructive-foreground hover:bg-destructive/90 disabled:opacity-50"
                    >
                      Confirm
                    </button>
                    <button
                      onclick={() => { confirmDeleteId = null; }}
                      class="rounded-md border border-border px-2 py-1 text-xs font-medium hover:bg-accent/50"
                    >
                      Cancel
                    </button>
                  {:else}
                    <button
                      onclick={() => { confirmDeleteId = session.id; }}
                      class="rounded-md border border-border px-2 py-1 text-xs font-medium text-muted-foreground hover:bg-accent/50 hover:text-foreground"
                    >
                      Delete
                    </button>
                  {/if}
                </div>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    {/if}
  </div>
</div>
