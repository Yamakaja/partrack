<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { listMembers, addMember, updateMemberRole, removeMember, searchUsers, type TeamMember, type UserSearchResult } from '$lib/api/teams.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import ConfirmDialog from '$lib/components/common/ConfirmDialog.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Select } from '$lib/components/ui/select/index.js';
  import { Dialog } from 'bits-ui';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { authStore } from '$lib/stores/auth.svelte.js';
  import { UserPlus, Trash2 } from 'lucide-svelte';

  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();

  let membersQuery = createQuery(() => ({
    queryKey: ['members', teamId],
    queryFn: () => listMembers(teamId),
  }));

  // Add member dialog
  let addOpen = $state(false);
  let newRole = $state('member');
  let adding = $state(false);

  // User search typeahead
  let userQuery = $state('');
  let userResults = $state<UserSearchResult[]>([]);
  let selectedUser = $state<UserSearchResult | null>(null);
  let searchOpen = $state(false);
  let debounceTimer: ReturnType<typeof setTimeout> | null = null;

  $effect(() => {
    const q = userQuery;
    if (debounceTimer) clearTimeout(debounceTimer);
    if (q.length >= 2 && !selectedUser) {
      debounceTimer = setTimeout(async () => {
        try {
          userResults = await searchUsers(q);
          searchOpen = userResults.length > 0;
        } catch {
          userResults = [];
        }
      }, 300);
    } else {
      userResults = [];
      if (!selectedUser) searchOpen = false;
    }
  });

  // Confirm remove dialog
  let confirmOpen = $state(false);
  let memberToRemove = $state<TeamMember | null>(null);
  let removing = $state(false);

  const roles = ['owner', 'admin', 'member', 'viewer'];

  function openAdd() {
    userQuery = '';
    userResults = [];
    selectedUser = null;
    searchOpen = false;
    newRole = 'member';
    addOpen = true;
  }

  async function handleAdd(e: Event) {
    e.preventDefault();
    if (!selectedUser) return;
    adding = true;
    try {
      await addMember(teamId, selectedUser.id, newRole);
      addOpen = false;
      qc.invalidateQueries({ queryKey: ['members', teamId] });
      pushToast('success', 'Member added');
    } catch (err: any) {
      pushToast('error', 'Failed to add member', err.message);
    } finally {
      adding = false;
    }
  }

  async function handleRoleChange(member: TeamMember, role: string) {
    try {
      await updateMemberRole(teamId, member.user_id, role);
      qc.invalidateQueries({ queryKey: ['members', teamId] });
      pushToast('success', 'Role updated');
    } catch (err: any) {
      pushToast('error', 'Failed to update role', err.message);
    }
  }

  function promptRemove(member: TeamMember) {
    memberToRemove = member;
    confirmOpen = true;
  }

  async function handleRemove() {
    if (!memberToRemove) return;
    removing = true;
    try {
      await removeMember(teamId, memberToRemove.user_id);
      qc.invalidateQueries({ queryKey: ['members', teamId] });
      pushToast('success', 'Member removed');
    } catch (err: any) {
      pushToast('error', 'Failed to remove member', err.message);
    } finally {
      removing = false;
      memberToRemove = null;
    }
  }

  let currentUserId = $derived(authStore.user?.id ?? '');
</script>

<div class="flex flex-col h-full">
  <Topbar title="Members">
    {#snippet actions()}
      <Button size="sm" onclick={openAdd}>
        <UserPlus class="h-4 w-4 mr-1" /> Add Member
      </Button>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6">
    {#if membersQuery.isPending}
      <div class="text-sm text-muted-foreground">Loading members…</div>
    {:else if membersQuery.isError}
      <div class="text-sm text-destructive">Error: {membersQuery.error.message}</div>
    {:else if membersQuery.data}
      {@const members = membersQuery.data.items}
      {#if members.length === 0}
        <EmptyState title="No members" description="Add members to collaborate on this team." />
      {:else}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Display Name</TableHead>
              <TableHead>Email</TableHead>
              <TableHead>Role</TableHead>
              <TableHead></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each members as member (member.user_id)}
              <TableRow>
                <TableCell class="font-medium">{member.display_name}</TableCell>
                <TableCell class="text-muted-foreground text-sm">{member.email}</TableCell>
                <TableCell>
                  <Select
                    value={member.role}
                    class="w-32"
                    disabled={member.user_id === currentUserId}
                    onchange={(e: Event) => handleRoleChange(member, (e.target as HTMLSelectElement).value)}
                  >
                    {#each roles as role}
                      <option value={role}>{role.charAt(0).toUpperCase() + role.slice(1)}</option>
                    {/each}
                  </Select>
                </TableCell>
                <TableCell>
                  {#if member.user_id !== currentUserId}
                    <Button
                      variant="ghost"
                      size="sm"
                      onclick={() => promptRemove(member)}
                      class="text-destructive hover:text-destructive"
                    >
                      <Trash2 class="h-4 w-4" />
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

<!-- Add Member Dialog -->
<Dialog.Root bind:open={addOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-4">Add Member</Dialog.Title>
      <form onsubmit={handleAdd} class="space-y-3">
        <div class="space-y-1">
          <Label for="member-search">User <span class="text-destructive">*</span></Label>
          <div class="relative">
            <Input
              id="member-search"
              value={userQuery}
              oninput={(e: Event) => {
                userQuery = (e.target as HTMLInputElement).value;
                selectedUser = null;
              }}
              onfocus={() => { if (userResults.length > 0) searchOpen = true; }}
              onblur={() => setTimeout(() => { searchOpen = false; }, 150)}
              placeholder="Search by name or email…"
              autocomplete="off"
            />
            {#if searchOpen && userResults.length > 0}
              <div class="absolute z-50 mt-1 w-full rounded-md border border-border bg-background shadow-md">
                {#each userResults as u (u.id)}
                  <button
                    type="button"
                    class="w-full px-3 py-2 text-left text-sm hover:bg-muted flex flex-col"
                    onmousedown={() => {
                      selectedUser = u;
                      userQuery = u.display_name;
                      searchOpen = false;
                    }}
                  >
                    <span class="font-medium">{u.display_name}</span>
                    <span class="text-xs text-muted-foreground">{u.email}</span>
                  </button>
                {/each}
              </div>
            {/if}
          </div>
          {#if selectedUser}
            <p class="text-xs text-muted-foreground">Selected: {selectedUser.display_name} ({selectedUser.email})</p>
          {:else if userQuery.length > 0 && userQuery.length < 2}
            <p class="text-xs text-muted-foreground">Type at least 2 characters to search</p>
          {/if}
        </div>
        <div class="space-y-1">
          <Label for="member-role">Role</Label>
          <Select id="member-role" bind:value={newRole}>
            {#each roles as role}
              <option value={role}>{role.charAt(0).toUpperCase() + role.slice(1)}</option>
            {/each}
          </Select>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" type="button" onclick={() => { addOpen = false; }}>Cancel</Button>
          <Button type="submit" disabled={adding || !selectedUser}>
            {adding ? 'Adding…' : 'Add Member'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<!-- Confirm Remove Dialog -->
<ConfirmDialog
  bind:open={confirmOpen}
  title="Remove Member"
  description={memberToRemove ? `Remove ${memberToRemove.display_name} from this team?` : ''}
  confirmLabel={removing ? 'Removing…' : 'Remove'}
  destructive
  onconfirm={handleRemove}
/>
