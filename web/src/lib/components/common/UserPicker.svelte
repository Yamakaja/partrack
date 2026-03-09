<script lang="ts">
  import { Popover } from 'bits-ui';
  import { ChevronDown, X } from 'lucide-svelte';
  import type { Snippet } from 'svelte';
  import { cn } from '$lib/utils/cn.js';
  import { avatarColor, avatarInitials } from '$lib/utils/avatarColor.js';
  import type { User } from '$lib/types/auth.js';

  let {
    users,
    selected = [],
    multiple = false,
    placeholder = 'Select user…',
    showInactive = false,
    onToggle,
    onClear,
    trigger,
  }: {
    users: User[];
    selected: string[];
    multiple?: boolean;
    placeholder?: string;
    showInactive?: boolean;
    onToggle: (userId: string) => void;
    onClear?: () => void;
    trigger?: Snippet<[{ open: boolean }]>;
  } = $props();

  let open = $state(false);
  let search = $state('');
  let searchInput = $state<HTMLInputElement | null>(null);

  function handleOpenChange(v: boolean) {
    if (!v) search = '';
    open = v;
  }

  const visibleUsers = $derived(
    showInactive ? users : users.filter((u) => u.is_active),
  );

  const filtered = $derived(
    search.trim()
      ? visibleUsers.filter(
          (u) =>
            u.display_name.toLowerCase().includes(search.trim().toLowerCase()) ||
            u.email.toLowerCase().includes(search.trim().toLowerCase()),
        )
      : visibleUsers,
  );

  const selectedSet = $derived(new Set(selected));
  const selectedCount = $derived(selected.length);

  const selectedUsers = $derived(users.filter((u) => selectedSet.has(u.id)));

  const triggerLabel = $derived.by(() => {
    if (selectedCount === 0) return placeholder;
    if (selectedCount === 1) return selectedUsers[0]?.display_name ?? placeholder;
    return `${selectedCount} users`;
  });

  function handleSelect(user: User) {
    const alreadySelected = selectedSet.has(user.id);

    if (alreadySelected) {
      // Single-select: only deselect if empty state is valid (onClear provided)
      if (!multiple) {
        if (onClear) onToggle(user.id);
        return;
      }
    }

    onToggle(user.id);

    if (!multiple) {
      open = false;
      search = '';
    }
  }

  function handleClear(e: MouseEvent) {
    e.stopPropagation();
    onClear?.();
  }

  $effect(() => {
    if (open && searchInput) {
      // Micro-tick to let the popover finish mounting
      requestAnimationFrame(() => searchInput?.focus());
    }
  });
</script>

<Popover.Root {open} onOpenChange={handleOpenChange}>
  <Popover.Trigger>
    {#if trigger}
      {@render trigger({ open })}
    {:else}
      <button
        class="flex h-8 w-full items-center gap-1.5 rounded-md border border-input bg-background px-2 text-xs text-left hover:bg-accent transition-colors"
      >
        <span class={cn('flex-1 truncate', selectedCount === 0 && 'text-muted-foreground')}>
          {triggerLabel}
        </span>
        {#if onClear && selectedCount > 0}
          <X
            class="h-3 w-3 shrink-0 text-muted-foreground hover:text-foreground"
            onclick={handleClear}
          />
        {:else}
          <ChevronDown class="h-3 w-3 shrink-0 text-muted-foreground" />
        {/if}
      </button>
    {/if}
  </Popover.Trigger>

  <Popover.Content
    align="start"
    class="z-50 w-64 rounded-md border border-border bg-popover text-popover-foreground shadow-md data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
  >
    <!-- Search -->
    <div class="border-b border-border px-2 py-1.5">
      <input
        bind:this={searchInput}
        type="search"
        bind:value={search}
        placeholder="Search…"
        class="w-full bg-transparent text-xs placeholder:text-muted-foreground focus:outline-none"
      />
    </div>

    <!-- List -->
    <div class="max-h-56 overflow-y-auto py-1 select-none">
      {#if filtered.length === 0}
        <p class="px-2 py-1.5 text-xs text-muted-foreground">No results</p>
      {:else}
        {#each filtered as user (user.id)}
          {@const isSelected = selectedSet.has(user.id)}
          {@const colors = avatarColor(user.id)}
          {@const initials = avatarInitials(user.display_name)}
          <button
            class={cn(
              'flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left hover:bg-accent hover:text-accent-foreground',
              !user.is_active && 'opacity-50',
              !multiple && isSelected && 'bg-accent',
            )}
            onclick={() => handleSelect(user)}
          >
            {#if multiple}
              <span
                class={cn(
                  'h-3.5 w-3.5 flex-shrink-0 rounded-sm border border-primary transition-colors',
                  isSelected && 'bg-primary',
                )}
              ></span>
            {/if}

            <!-- Avatar -->
            <span
              class={cn(
                'flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full text-[10px] font-semibold',
                colors.bg,
                colors.text,
              )}
            >
              {initials}
            </span>

            <!-- Name + email -->
            <span class="flex min-w-0 flex-1 flex-col">
              <span class="flex items-center gap-1.5 text-xs font-medium leading-tight truncate">
                {user.display_name}
                {#if !user.is_active}
                  <span class="rounded px-1 py-px text-[10px] bg-muted text-muted-foreground font-normal shrink-0">
                    Inactive
                  </span>
                {/if}
              </span>
              <span class="text-[11px] text-muted-foreground truncate leading-tight">
                {user.email}
              </span>
            </span>
          </button>
        {/each}
      {/if}
    </div>

    <!-- Footer (multi-select only) -->
    {#if multiple && selectedCount > 0}
      <div class="border-t border-border px-2 py-1.5 flex items-center justify-between">
        <span class="text-xs text-muted-foreground">{selectedCount} selected</span>
        {#if onClear}
          <button
            class="text-xs text-muted-foreground hover:text-foreground underline underline-offset-2"
            onclick={() => { onClear?.(); open = false; }}
          >
            Clear
          </button>
        {/if}
      </div>
    {/if}
  </Popover.Content>
</Popover.Root>
