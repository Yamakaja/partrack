<script lang="ts">
  import { Badge } from '$lib/components/ui/badge/index.js';
  import Skeleton from '$lib/components/ui/skeleton/Skeleton.svelte';
  import { navigate } from '$lib/router/index.svelte.js';
  import type { Build, BuildStatus } from '$lib/types/orders.js';

  let {
    teamId,
    pipeline,
    currentUserId = undefined,
  }: {
    teamId: string;
    pipeline: { draft: Build[]; pending: Build[]; ordered: Build[]; ready: Build[] } | undefined;
    currentUserId?: string;
  } = $props();

  function statusVariant(status: BuildStatus): 'secondary' | 'default' | 'warning' | 'success' | 'destructive' {
    switch (status) {
      case 'draft':     return 'secondary';
      case 'pending':   return 'default';
      case 'ordered':   return 'warning';
      case 'ready':     return 'success';
      case 'assembled': return 'secondary';
      case 'cancelled': return 'destructive';
      default:          return 'secondary';
    }
  }

  function sortUserFirst(builds: Build[]): Build[] {
    return [...builds].sort((a, b) => {
      const aMe = a.created_by === currentUserId ? 0 : 1;
      const bMe = b.created_by === currentUserId ? 0 : 1;
      return aMe - bMe;
    });
  }

  const pipelineCards = $derived(pipeline ? [
    { status: 'draft'   as BuildStatus, label: 'Draft',   builds: sortUserFirst(pipeline.draft),   empty: 'No drafts' },
    { status: 'pending' as BuildStatus, label: 'Pending', builds: sortUserFirst(pipeline.pending), empty: 'Nothing pending' },
    { status: 'ordered' as BuildStatus, label: 'Ordered', builds: sortUserFirst(pipeline.ordered), empty: 'No active orders' },
    { status: 'ready'   as BuildStatus, label: 'Ready',   builds: sortUserFirst(pipeline.ready),   empty: 'All assembled' },
  ] : null);
</script>

{#if pipelineCards === null}
  <div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
    {#each [0, 1, 2, 3] as _}
      <Skeleton class="h-48 rounded-lg" />
    {/each}
  </div>
{:else}
  <div class="grid grid-cols-2 lg:grid-cols-4 gap-4">
    {#each pipelineCards as card}
      <div class="rounded-lg border border-border bg-card flex flex-col">
        <!-- Card header -->
        <div class="flex items-center justify-between px-3 py-2 border-b border-border">
          <span class="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            {card.label}
          </span>
          {#if card.builds.length > 0}
            <Badge variant={statusVariant(card.status)} class="text-xs px-1.5 py-0 h-4">
              {card.builds.length}
            </Badge>
          {/if}
        </div>
        <!-- Card body -->
        <div class="overflow-y-auto max-h-60 p-1.5 flex-1">
          {#if card.builds.length === 0}
            <p class="text-xs text-muted-foreground text-center py-4">{card.empty}</p>
          {:else}
            {#each card.builds as build (build.id)}
              {@const isMe = build.created_by === currentUserId}
              {@const muted = card.status === 'pending' && build.excluded_from_cart}
              <button
                class="w-full text-left rounded px-2 py-1.5 text-xs hover:bg-muted transition-colors flex items-start gap-1 {muted ? 'text-muted-foreground italic' : ''}"
                onclick={() => navigate(`/teams/${teamId}/builds/${build.id}`)}
              >
                {#if isMe}
                  <span class="text-primary mt-px shrink-0" title="Your build">●</span>
                {:else}
                  <span class="shrink-0 w-2"></span>
                {/if}
                <span class="flex-1 min-w-0">
                  <span class="flex items-baseline justify-between gap-1">
                    <span class="font-medium truncate">{build.project_name}</span>
                    <span class="text-muted-foreground/70 shrink-0 text-[10px]">
                      {isMe ? 'You' : (build.created_by_display_name ?? '—')}
                    </span>
                  </span>
                  <span class="text-muted-foreground truncate block">
                    {build.bom_revision_label} · {build.quantity}×
                  </span>
                </span>
              </button>
            {/each}
          {/if}
        </div>
      </div>
    {/each}
  </div>
{/if}
