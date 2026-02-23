<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import {
    listUsers,
    createUser,
    updateUser,
    setPassword,
    generateResetLink,
    type AdminUser,
  } from '$lib/api/admin.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import ConfirmDialog from '$lib/components/common/ConfirmDialog.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Dialog } from 'bits-ui';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { UserPlus } from 'lucide-svelte';

  const qc = useQueryClient();

  let usersQuery = createQuery(() => ({
    queryKey: ['admin', 'users'],
    queryFn: () => listUsers(),
  }));

  // ── Add user dialog ─────────────────────────────────────────────────────────
  let addOpen = $state(false);
  let newDisplayName = $state('');
  let newEmail = $state('');
  let newPassword = $state('');
  let adding = $state(false);

  function openAdd() {
    newDisplayName = '';
    newEmail = '';
    newPassword = '';
    addOpen = true;
  }

  async function handleAdd(e: Event) {
    e.preventDefault();
    adding = true;
    try {
      await createUser({
        display_name: newDisplayName,
        email: newEmail,
        password: newPassword || undefined,
      });
      addOpen = false;
      qc.invalidateQueries({ queryKey: ['admin', 'users'] });
      pushToast('success', 'User created');
    } catch (err: any) {
      pushToast('error', 'Failed to create user', err.message);
    } finally {
      adding = false;
    }
  }

  // ── Set password dialog ──────────────────────────────────────────────────────
  let pwOpen = $state(false);
  let pwUser = $state<AdminUser | null>(null);
  let newPw = $state('');
  let settingPw = $state(false);

  function openSetPassword(u: AdminUser) {
    pwUser = u;
    newPw = '';
    pwOpen = true;
  }

  async function handleSetPassword(e: Event) {
    e.preventDefault();
    if (!pwUser) return;
    settingPw = true;
    try {
      await setPassword(pwUser.id, newPw);
      pwOpen = false;
      qc.invalidateQueries({ queryKey: ['admin', 'users'] });
      pushToast('success', 'Password updated');
    } catch (err: any) {
      pushToast('error', 'Failed to set password', err.message);
    } finally {
      settingPw = false;
    }
  }

  // ── Edit dialog ──────────────────────────────────────────────────────────────
  let editOpen = $state(false);
  let editUser = $state<AdminUser | null>(null);
  let editDisplayName = $state('');
  let editEmail = $state('');
  let editIsAdmin = $state(false);
  let editing = $state(false);

  function openEdit(u: AdminUser) {
    editUser = u;
    editDisplayName = u.display_name;
    editEmail = u.email;
    editIsAdmin = u.is_admin;
    editOpen = true;
  }

  async function handleEdit(e: Event) {
    e.preventDefault();
    if (!editUser) return;
    editing = true;
    try {
      await updateUser(editUser.id, {
        display_name: editDisplayName,
        email: editEmail,
        is_admin: editIsAdmin,
      });
      editOpen = false;
      qc.invalidateQueries({ queryKey: ['admin', 'users'] });
      pushToast('success', 'User updated');
    } catch (err: any) {
      pushToast('error', 'Failed to update user', err.message);
    } finally {
      editing = false;
    }
  }

  // ── Deactivate / Reactivate confirm ─────────────────────────────────────────
  let confirmOpen = $state(false);
  let confirmUser = $state<AdminUser | null>(null);
  let toggling = $state(false);

  function promptToggle(u: AdminUser) {
    confirmUser = u;
    confirmOpen = true;
  }

  async function handleToggleActive() {
    if (!confirmUser) return;
    toggling = true;
    try {
      await updateUser(confirmUser.id, { is_active: !confirmUser.is_active });
      qc.invalidateQueries({ queryKey: ['admin', 'users'] });
      pushToast('success', confirmUser.is_active ? 'User deactivated' : 'User reactivated');
    } catch (err: any) {
      pushToast('error', 'Failed to update user', err.message);
    } finally {
      toggling = false;
      confirmUser = null;
    }
  }

  // ── Generate reset link ──────────────────────────────────────────────────────
  let linkOpen = $state(false);
  let resetLink = $state('');
  let resetLinkExpiry = $state('');

  async function handleGenerateLink(u: AdminUser) {
    try {
      const res = await generateResetLink(u.id);
      resetLink = window.location.origin + res.path;
      resetLinkExpiry = new Date(res.expires_at).toLocaleString();
      linkOpen = true;
    } catch (err: any) {
      pushToast('error', 'Failed to generate link', err.message);
    }
  }

  function copyLink() {
    navigator.clipboard.writeText(resetLink).then(() => {
      pushToast('success', 'Link copied to clipboard');
    });
  }
</script>

<div class="flex flex-col h-full">
  <Topbar title="Users">
    {#snippet actions()}
      <Button size="sm" onclick={openAdd}>
        <UserPlus class="h-4 w-4 mr-1" /> Add User
      </Button>
    {/snippet}
  </Topbar>

  <div class="flex-1 overflow-auto p-6">
    {#if usersQuery.isPending}
      <div class="text-sm text-muted-foreground">Loading users…</div>
    {:else if usersQuery.isError}
      <div class="text-sm text-destructive">Error: {usersQuery.error.message}</div>
    {:else if usersQuery.data}
      {@const users = usersQuery.data.items}
      {#if users.length === 0}
        <EmptyState title="No users" description="Create the first user account." />
      {:else}
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Display Name</TableHead>
              <TableHead>Email</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Admin</TableHead>
              <TableHead>Password</TableHead>
              <TableHead>Created</TableHead>
              <TableHead></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each users as u (u.id)}
              <TableRow>
                <TableCell class="font-medium">{u.display_name}</TableCell>
                <TableCell class="text-muted-foreground text-sm">{u.email}</TableCell>
                <TableCell>
                  <span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium
                    {u.is_active ? 'bg-green-100 text-green-800 dark:bg-green-900/30 dark:text-green-400' : 'bg-muted text-muted-foreground'}">
                    {u.is_active ? 'Active' : 'Inactive'}
                  </span>
                </TableCell>
                <TableCell>
                  {#if u.is_admin}
                    <span class="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium bg-blue-100 text-blue-800 dark:bg-blue-900/30 dark:text-blue-400">
                      Admin
                    </span>
                  {/if}
                </TableCell>
                <TableCell class="text-sm text-muted-foreground">
                  {u.has_password ? 'Set' : '—'}
                </TableCell>
                <TableCell class="text-sm text-muted-foreground">
                  {new Date(u.created_at).toLocaleDateString()}
                </TableCell>
                <TableCell>
                  <div class="flex items-center gap-1">
                    <Button variant="ghost" size="sm" onclick={() => openEdit(u)}>Edit</Button>
                    <Button variant="ghost" size="sm" onclick={() => openSetPassword(u)}>Set Password</Button>
                    <Button variant="ghost" size="sm" onclick={() => handleGenerateLink(u)}>Reset Link</Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      class={u.is_active ? 'text-destructive hover:text-destructive' : ''}
                      onclick={() => promptToggle(u)}
                    >
                      {u.is_active ? 'Deactivate' : 'Reactivate'}
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            {/each}
          </TableBody>
        </Table>
      {/if}
    {/if}
  </div>
</div>

<!-- Add User Dialog -->
<Dialog.Root bind:open={addOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-4">Add User</Dialog.Title>
      <form onsubmit={handleAdd} class="space-y-3">
        <div class="space-y-1">
          <Label for="new-display-name">Display Name <span class="text-destructive">*</span></Label>
          <Input id="new-display-name" bind:value={newDisplayName} required placeholder="Jane Doe" />
        </div>
        <div class="space-y-1">
          <Label for="new-email">Email <span class="text-destructive">*</span></Label>
          <Input id="new-email" type="email" bind:value={newEmail} required placeholder="jane@example.com" />
        </div>
        <div class="space-y-1">
          <Label for="new-password">Password <span class="text-muted-foreground text-xs">(optional — user can use a reset link instead)</span></Label>
          <Input id="new-password" type="password" bind:value={newPassword} autocomplete="new-password" />
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" type="button" onclick={() => { addOpen = false; }}>Cancel</Button>
          <Button type="submit" disabled={adding || !newDisplayName || !newEmail}>
            {adding ? 'Creating…' : 'Create User'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<!-- Set Password Dialog -->
<Dialog.Root bind:open={pwOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-1">Set Password</Dialog.Title>
      {#if pwUser}
        <p class="text-sm text-muted-foreground mb-4">For {pwUser.display_name}</p>
      {/if}
      <form onsubmit={handleSetPassword} class="space-y-3">
        <div class="space-y-1">
          <Label for="pw-new">New Password <span class="text-destructive">*</span></Label>
          <Input id="pw-new" type="password" bind:value={newPw} required autocomplete="new-password" />
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" type="button" onclick={() => { pwOpen = false; }}>Cancel</Button>
          <Button type="submit" disabled={settingPw || !newPw}>
            {settingPw ? 'Saving…' : 'Set Password'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<!-- Edit User Dialog -->
<Dialog.Root bind:open={editOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-4">Edit User</Dialog.Title>
      <form onsubmit={handleEdit} class="space-y-3">
        <div class="space-y-1">
          <Label for="edit-display-name">Display Name</Label>
          <Input id="edit-display-name" bind:value={editDisplayName} required />
        </div>
        <div class="space-y-1">
          <Label for="edit-email">Email</Label>
          <Input id="edit-email" type="email" bind:value={editEmail} required />
        </div>
        <div class="flex items-center gap-2">
          <input id="edit-is-admin" type="checkbox" bind:checked={editIsAdmin} class="h-4 w-4 rounded border-input" />
          <Label for="edit-is-admin">Instance admin</Label>
        </div>
        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" type="button" onclick={() => { editOpen = false; }}>Cancel</Button>
          <Button type="submit" disabled={editing}>
            {editing ? 'Saving…' : 'Save'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<!-- Reset Link Dialog -->
<Dialog.Root bind:open={linkOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-1">Password Reset Link</Dialog.Title>
      <p class="text-sm text-muted-foreground mb-4">Share this link with the user. It expires at {resetLinkExpiry}.</p>
      <div class="flex gap-2">
        <input
          type="text"
          readonly
          value={resetLink}
          class="flex-1 rounded-md border border-input bg-muted px-3 py-2 text-sm font-mono select-all"
        />
        <Button variant="outline" onclick={copyLink}>Copy</Button>
      </div>
      <div class="flex justify-end pt-4">
        <Button onclick={() => { linkOpen = false; }}>Done</Button>
      </div>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<!-- Deactivate / Reactivate Confirm -->
<ConfirmDialog
  bind:open={confirmOpen}
  title={confirmUser?.is_active ? 'Deactivate User' : 'Reactivate User'}
  description={confirmUser
    ? confirmUser.is_active
      ? `Deactivate ${confirmUser.display_name}? All their sessions will be revoked immediately.`
      : `Reactivate ${confirmUser.display_name}?`
    : ''}
  confirmLabel={toggling ? 'Saving…' : confirmUser?.is_active ? 'Deactivate' : 'Reactivate'}
  destructive={confirmUser?.is_active ?? false}
  onconfirm={handleToggleActive}
/>
