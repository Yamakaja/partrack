<script lang="ts">
  import { createQuery } from '@tanstack/svelte-query';
  import { FolderKanban } from 'lucide-svelte';
  import { getProject } from '$lib/api/projects.js';
  import { linkAction } from '$lib/router/index.svelte.js';
  import { statusMeta, TONE_BADGE, TONE_BG } from '$lib/objectref/registry.js';
  import Badge from '$lib/components/ui/badge/Badge.svelte';
  import CardShell from './CardShell.svelte';

  let {
    id,
    teamId,
    open,
  }: {
    id: string;
    teamId: string;
    open: boolean;
  } = $props();

  const query = createQuery(() => ({
    queryKey: ['project', teamId, id],
    queryFn: () => getProject(teamId, id),
    enabled: open,
    staleTime: 60_000,
  }));

  const project = $derived(query.data);
  const status = $derived(statusMeta('project', project?.status));

  // Standing: how completely the active BOM resolves to real parts. Unresolved
  // and ambiguous lines are the work still owed before the project can be built.
  const resolution = $derived.by(() => {
    const s = project?.active_revision_summary;
    if (!s || s.item_count <= 0) return null;
    return {
      ...s,
      resolvedPct: (s.resolved_count / s.item_count) * 100,
      ambiguousPct: (s.ambiguous_count / s.item_count) * 100,
    };
  });

  function shortDate(iso: string | null | undefined): string {
    if (!iso) return '—';
    return new Date(iso).toLocaleDateString(undefined, { day: 'numeric', month: 'short', year: 'numeric' });
  }
</script>

<CardShell pending={query.isPending} error={query.isError} errorText="Couldn't load project details.">
  {#if project}
    <div class="flex gap-3 p-3.5">
      <div class="flex h-14 w-14 shrink-0 items-center justify-center rounded-md border border-border bg-muted">
        <FolderKanban class="h-6 w-6 text-muted-foreground/50" />
      </div>
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2">
          <span class="text-[13px] font-semibold">{project.name}</span>
          {#if status}
            <Badge variant={TONE_BADGE[status.tone] as never}>{status.label}</Badge>
          {/if}
        </div>
        <p class="truncate text-[11.5px] text-muted-foreground">
          {#if project.project_manager_display_name}{project.project_manager_display_name} · {/if}created
          {shortDate(project.created_at)}
        </p>
        {#if project.description}
          <p class="mt-1.5 line-clamp-2 text-xs leading-relaxed">{project.description}</p>
        {/if}
      </div>
    </div>

    <div class="mx-3.5 grid grid-cols-2 gap-x-4 gap-y-2 border-t border-border py-2.5">
      <div class="min-w-0">
        <p class="text-[10px] uppercase tracking-wider text-muted-foreground">Active revision</p>
        <p class="truncate text-[11.5px]">{project.active_bom_revision_label ?? 'none'}</p>
      </div>
      {#if resolution}
        <div class="min-w-0">
          <p class="text-[10px] uppercase tracking-wider text-muted-foreground">Line items</p>
          <p class="truncate font-mono text-[11.5px] tabular-nums">{resolution.item_count}</p>
        </div>
      {/if}
    </div>

    {#if resolution}
      <div class="border-t border-border px-3.5 py-2">
        <div class="flex items-center gap-2 text-[11.5px]">
          <span
            class="h-[7px] w-[7px] shrink-0 rounded-full {resolution.unresolved_count === 0
              ? TONE_BG.success
              : TONE_BG.warning}"
          ></span>
          <span>
            <span class="font-mono font-semibold tabular-nums">{resolution.resolved_count}</span>
            of
            <span class="font-mono font-semibold tabular-nums">{resolution.item_count}</span>
            lines resolved
          </span>
          {#if resolution.ambiguous_count > 0}
            <span class="ml-auto text-muted-foreground">{resolution.ambiguous_count} need a decision</span>
          {/if}
        </div>
        <div class="mt-1.5 flex h-[5px] overflow-hidden rounded-full bg-muted">
          <span class="h-full {TONE_BG.success}" style="width: {resolution.resolvedPct}%"></span>
          <span class="h-full {TONE_BG.attention}" style="width: {resolution.ambiguousPct}%"></span>
        </div>
      </div>
    {/if}

    <div class="flex items-center gap-3.5 border-t border-border px-3.5 py-2 text-[11.5px] font-medium">
      <a
        use:linkAction
        href="/teams/{teamId}/projects/{id}"
        class="ml-auto text-muted-foreground hover:text-foreground"
      >
        Open project →
      </a>
    </div>
  {/if}
</CardShell>
