<script lang="ts">
  import { createQuery, useQueryClient } from '@tanstack/svelte-query';
  import { listDistributors, listDistributorConfigs, upsertDistributorConfig, deleteDistributorConfig, getCredentialSpec } from '$lib/api/distributors.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import ConfirmDialog from '$lib/components/common/ConfirmDialog.svelte';
  import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '$lib/components/ui/table/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Dialog } from 'bits-ui';
  import { pushToast } from '$lib/stores/toasts.svelte.js';
  import { Settings } from 'lucide-svelte';
  import type { Distributor, DistributorConfig, CredentialField } from '$lib/types/parts.js';

  let { teamId }: { teamId: string } = $props();

  const qc = useQueryClient();

  let distributorsQuery = createQuery(() => ({
    queryKey: ['distributors'],
    queryFn: () => listDistributors(),
  }));

  let configsQuery = createQuery(() => ({
    queryKey: ['distributor-configs', teamId],
    queryFn: () => listDistributorConfigs(teamId),
  }));

  // Combined display list
  let displayList = $derived.by(() => {
    const dists = distributorsQuery.data?.items ?? [];
    const configs = configsQuery.data?.items ?? [];
    const configMap = new Map(configs.map(c => [c.distributor_id, c]));
    return dists.map(d => ({
      distributor: d,
      config: configMap.get(d.id) ?? null,
    }));
  });

  // Configure dialog state
  let configureOpen = $state(false);
  let configuringDistributor = $state<Distributor | null>(null);
  let cfgEnabled = $state(false);
  let cfgPreferred = $state(false);
  let cfgCacheDuration = $state('3600');
  let configHasCredentials = $state(false);
  let saving = $state(false);

  // Credential spec state
  let credentialSpec = $state<CredentialField[] | null>(null);
  let specLoading = $state(false);
  let credentialValues = $state<Record<string, string | boolean>>({});
  let showSecret = $state<Record<string, boolean>>({});

  // Delete confirm
  let deleteConfirmOpen = $state(false);
  let deletingDistributor = $state<Distributor | null>(null);
  let deleting = $state(false);

  function openConfigure(dist: Distributor, config: DistributorConfig | null) {
    configuringDistributor = dist;
    cfgEnabled = config?.enabled ?? false;
    cfgPreferred = config?.preferred ?? false;
    cfgCacheDuration = String(config?.cache_duration_seconds ?? 3600);
    configHasCredentials = config?.has_credentials ?? false;
    credentialSpec = null;
    credentialValues = {};
    showSecret = {};
    configureOpen = true;

    specLoading = true;
    getCredentialSpec(dist.id)
      .then(spec => {
        credentialSpec = spec ?? [];
        credentialValues = Object.fromEntries(
          (spec ?? []).map(f => [f.key, f.type === 'bool' ? false : ''])
        );
      })
      .catch(() => { credentialSpec = []; })
      .finally(() => { specLoading = false; });
  }

  async function handleSaveConfig(e: Event) {
    e.preventDefault();
    if (!configuringDistributor) return;
    const cacheDuration = parseInt(cfgCacheDuration, 10);
    if (isNaN(cacheDuration) || cacheDuration < 0) {
      pushToast('error', 'Invalid cache duration');
      return;
    }

    let credentials: Record<string, unknown> | null | undefined = undefined;
    if (credentialSpec && credentialSpec.length > 0) {
      const obj: Record<string, unknown> = {};
      for (const field of credentialSpec) {
        const val = credentialValues[field.key];
        if (field.type === 'bool') {
          obj[field.key] = val;
        } else if (typeof val === 'string' && val.trim() !== '') {
          obj[field.key] = val.trim();
        }
        // skip empty optional fields; empty required fields are left out (server validates)
      }
      // Only submit credentials if at least one field has a non-default value
      if (Object.entries(obj).some(([, v]) => v !== '' && v !== false)) {
        credentials = obj;
      }
    }

    saving = true;
    try {
      await upsertDistributorConfig(teamId, configuringDistributor.id, {
        enabled: cfgEnabled,
        preferred: cfgPreferred,
        cache_duration_seconds: cacheDuration,
        api_credentials: credentials,
      });
      configureOpen = false;
      qc.invalidateQueries({ queryKey: ['distributor-configs', teamId] });
      pushToast('success', 'Distributor configuration saved');
    } catch (err: any) {
      pushToast('error', 'Failed to save configuration', err.message);
    } finally {
      saving = false;
    }
  }

  function promptDeleteConfig(dist: Distributor) {
    deletingDistributor = dist;
    deleteConfirmOpen = true;
  }

  async function handleDeleteConfig() {
    if (!deletingDistributor) return;
    deleting = true;
    try {
      await deleteDistributorConfig(teamId, deletingDistributor.id);
      qc.invalidateQueries({ queryKey: ['distributor-configs', teamId] });
      pushToast('success', 'Distributor configuration removed');
    } catch (err: any) {
      pushToast('error', 'Failed to remove configuration', err.message);
    } finally {
      deleting = false;
      deletingDistributor = null;
    }
  }
</script>

<div class="flex flex-col h-full">
  <Topbar title="Distributors" />

  <div class="flex-1 overflow-auto p-6">
    {#if distributorsQuery.isPending || configsQuery.isPending}
      <div class="text-sm text-muted-foreground">Loading…</div>
    {:else if distributorsQuery.isError}
      <div class="text-sm text-destructive">Error: {distributorsQuery.error.message}</div>
    {:else}
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Distributor</TableHead>
            <TableHead>API Type</TableHead>
            <TableHead>Enabled</TableHead>
            <TableHead>Preferred</TableHead>
            <TableHead>Cache Duration</TableHead>
            <TableHead>Actions</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {#each displayList as { distributor, config } (distributor.id)}
            <TableRow>
              <TableCell class="font-medium">{distributor.name}</TableCell>
              <TableCell class="text-muted-foreground text-sm">{distributor.api_type ?? '—'}</TableCell>
              <TableCell>
                {#if config}
                  <Badge variant={config.enabled ? 'success' : 'secondary'}>
                    {config.enabled ? 'Enabled' : 'Disabled'}
                  </Badge>
                {:else}
                  <Badge variant="secondary">Not configured</Badge>
                {/if}
              </TableCell>
              <TableCell>
                {#if config?.preferred}
                  <Badge variant="default">Preferred</Badge>
                {:else}
                  <span class="text-muted-foreground text-sm">—</span>
                {/if}
              </TableCell>
              <TableCell class="text-muted-foreground text-sm">
                {config ? `${config.cache_duration_seconds}s` : '—'}
              </TableCell>
              <TableCell>
                <div class="flex items-center gap-1">
                  <Button
                    variant="ghost"
                    size="sm"
                    onclick={() => openConfigure(distributor, config)}
                  >
                    <Settings class="h-4 w-4 mr-1" /> Configure
                  </Button>
                  {#if config}
                    <Button
                      variant="ghost"
                      size="sm"
                      onclick={() => promptDeleteConfig(distributor)}
                      class="text-destructive hover:text-destructive"
                    >
                      Remove
                    </Button>
                  {/if}
                </div>
              </TableCell>
            </TableRow>
          {/each}
        </TableBody>
      </Table>
    {/if}
  </div>
</div>

<!-- Configure Dialog -->
<Dialog.Root bind:open={configureOpen}>
  <Dialog.Portal>
    <Dialog.Overlay class="fixed inset-0 z-50 bg-black/80" />
    <Dialog.Content class="fixed left-1/2 top-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg">
      <Dialog.Title class="text-lg font-semibold mb-4">
        Configure {configuringDistributor?.name ?? 'Distributor'}
      </Dialog.Title>
      <form onsubmit={handleSaveConfig} class="space-y-3">
        <label class="flex items-center gap-2 text-sm cursor-pointer select-none">
          <input
            type="checkbox"
            bind:checked={cfgEnabled}
            class="h-4 w-4 rounded border-input"
          />
          Enabled
        </label>
        <label class="flex items-center gap-2 text-sm cursor-pointer select-none">
          <input
            type="checkbox"
            bind:checked={cfgPreferred}
            class="h-4 w-4 rounded border-input"
          />
          Preferred
        </label>
        <div class="space-y-1">
          <Label for="cache-duration">Cache Duration (seconds)</Label>
          <Input
            id="cache-duration"
            type="number"
            bind:value={cfgCacheDuration}
            min="0"
            placeholder="3600"
          />
        </div>

        <!-- Credential fields -->
        {#if specLoading}
          <p class="text-xs text-muted-foreground">Loading credential fields…</p>
        {:else if credentialSpec !== null && credentialSpec.length === 0}
          {#if configuringDistributor?.api_type}
            <p class="text-xs text-muted-foreground">No API credentials required for this distributor.</p>
          {/if}
        {:else if credentialSpec !== null && credentialSpec.length > 0}
          {#if configHasCredentials}
            <p class="text-xs text-muted-foreground">Credentials are already configured. Fill in all fields to replace them, or leave blank to keep existing.</p>
          {/if}
          {#each credentialSpec as field (field.key)}
            <div class="space-y-1">
              <Label for={`cred-${field.key}`}>{field.label}{field.required ? ' *' : ''}</Label>
              {#if field.type === 'bool'}
                <label class="flex items-center gap-2 text-sm cursor-pointer select-none">
                  <input
                    type="checkbox"
                    id={`cred-${field.key}`}
                    bind:checked={credentialValues[field.key] as boolean}
                    class="h-4 w-4 rounded border-input"
                  />
                  {field.label}
                </label>
              {:else}
                <div class="relative">
                  <Input
                    id={`cred-${field.key}`}
                    type={field.secret && !showSecret[field.key] ? 'password' : 'text'}
                    bind:value={credentialValues[field.key] as string}
                    placeholder={field.placeholder ?? ''}
                  />
                  {#if field.secret}
                    <button
                      type="button"
                      class="absolute right-2 top-1/2 -translate-y-1/2 text-xs text-muted-foreground hover:text-foreground"
                      onclick={() => { showSecret[field.key] = !showSecret[field.key]; }}
                    >{showSecret[field.key] ? 'Hide' : 'Show'}</button>
                  {/if}
                </div>
              {/if}
              {#if field.description}
                <p class="text-xs text-muted-foreground">{field.description}</p>
              {/if}
            </div>
          {/each}
        {/if}

        <div class="flex justify-end gap-2 pt-2">
          <Button variant="outline" type="button" onclick={() => { configureOpen = false; }}>Cancel</Button>
          <Button type="submit" disabled={saving}>
            {saving ? 'Saving…' : 'Save'}
          </Button>
        </div>
      </form>
    </Dialog.Content>
  </Dialog.Portal>
</Dialog.Root>

<!-- Delete Config Confirm -->
<ConfirmDialog
  bind:open={deleteConfirmOpen}
  title="Remove Configuration"
  description={deletingDistributor ? `Remove configuration for ${deletingDistributor.name}?` : ''}
  confirmLabel={deleting ? 'Removing…' : 'Remove'}
  destructive
  onconfirm={handleDeleteConfig}
/>
