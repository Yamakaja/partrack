<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import { GitBranch } from 'lucide-svelte';
  import { listRevisions } from '$lib/api/revisions.js';
  import { listProjects } from '$lib/api/projects.js';
  import { linkAction } from '$lib/router/index.svelte.js';
  import { revisionLabel, TONE_BG } from '$lib/objectref/registry.js';
  import Badge from '$lib/components/ui/badge/Badge.svelte';
  import CardShell from './CardShell.svelte';

  // A revision lives under its project, so the card needs both ids.
  let {
    id,
    parentId,
    teamId,
    open,
  }: {
    id: string;
    parentId: string;
    teamId: string;
    open: boolean;
  } = $props();

  // Deliberately the *list* query, not getRevision: the detail endpoint returns
  // the entire BOM with substitutes and enrichment blobs, and drops the very
  // counts this card exists to show. Revisions per project are few, and this is
  // the same key the project page already caches.
  const query = createQuery(() => ({
    queryKey: ['revisions', teamId, parentId],
    queryFn: () => listRevisions(teamId, parentId),
    enabled: open && !!parentId,
    staleTime: 60_000,
  }));

  // The project name completes the identity; the whole list is one cached call.
  const projectsQuery = createQuery(() => ({
    queryKey: ['projects', teamId],
    queryFn: () => listProjects(teamId),
    enabled: open,
    staleTime: 60_000,
  }));

  const revision = $derived(query.data?.items?.find((r) => r.id === id) ?? null);
  const projectName = $derived(
    projectsQuery.data?.items?.find((p) => p.id === parentId)?.name ?? '',
  );
  const title = $derived(revision ? revisionLabel(projectName, revision.label) : '');

  const resolution = $derived.by(() => {
    if (!revision || revision.item_count <= 0) return null;
    return { pct: (revision.resolved_count / revision.item_count) * 100 };
  });

  function shortDate(iso: string | null | undefined): string {
    if (!iso) return '—';
    return new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
  }
</script>

<CardShell
  pending={query.isPending}
  error={query.isError || (!query.isPending && !revision)}
  errorText="Couldn't load revision details."
>
  {#if revision}
    <div class="flex gap-3 p-3.5">
      <div class="flex h-14 w-14 shrink-0 items-center justify-center rounded-md border border-border bg-muted">
        <GitBranch class="h-6 w-6 text-muted-foreground/50" />
      </div>
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2">
          <span class="text-[13px] font-semibold">{title}</span>
          {#if revision.is_active}
            <Badge variant="success">Active</Badge>
          {/if}
        </div>
        <p class="truncate text-[11.5px] text-muted-foreground">
          imported by {revision.imported_by_display_name} · {shortDate(revision.imported_at)}
        </p>
        {#if revision.notes}
          <p class="mt-1.5 line-clamp-2 text-xs leading-relaxed">{revision.notes}</p>
        {/if}
      </div>
    </div>

    <div class="mx-3.5 grid grid-cols-2 gap-x-4 gap-y-2 border-t border-border py-2.5">
      <div class="min-w-0">
        <p class="text-[10px] uppercase tracking-wider text-muted-foreground">Line items</p>
        <p class="truncate font-mono text-[11.5px] tabular-nums">{revision.item_count}</p>
      </div>
      <div class="min-w-0">
        <p class="text-[10px] uppercase tracking-wider text-muted-foreground">Resolved</p>
        <p class="truncate font-mono text-[11.5px] tabular-nums">{revision.resolved_count}</p>
      </div>
    </div>

    {#if resolution}
      <div class="border-t border-border px-3.5 py-2">
        <div class="flex items-center gap-2 text-[11.5px]">
          <span
            class="h-[7px] w-[7px] shrink-0 rounded-full {revision.unresolved_count === 0
              ? TONE_BG.success
              : TONE_BG.warning}"
          ></span>
          <span>
            {#if revision.unresolved_count === 0}
              every line resolved
            {:else}
              <span class="font-mono font-semibold tabular-nums">{revision.unresolved_count}</span>
              line{revision.unresolved_count === 1 ? '' : 's'} unresolved
            {/if}
          </span>
        </div>
        <div class="mt-1.5 h-[5px] overflow-hidden rounded-full bg-muted">
          <div class="h-full {TONE_BG.success}" style="width: {resolution.pct}%"></div>
        </div>
      </div>
    {/if}

    <div class="flex items-center gap-3.5 border-t border-border px-3.5 py-2 text-[11.5px] font-medium">
      <a use:linkAction href="/teams/{teamId}/projects/{parentId}" class="text-info hover:underline">
        Open project →
      </a>
      <a
        use:linkAction
        href="/teams/{teamId}/projects/{parentId}/revisions/{id}"
        class="ml-auto text-muted-foreground hover:text-foreground"
      >
        Open revision →
      </a>
    </div>
  {/if}
</CardShell>
