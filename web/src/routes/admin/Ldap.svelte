<script lang="ts">
  import { onMount } from 'svelte';
  import {
    listLdapProviders,
    diagnoseLdap,
    testLdapConnection,
    type LdapProviderInfo,
    type LdapDiagnoseResult,
    type LdapConnectionResult,
  } from '$lib/api/admin.js';
  import Topbar from '$lib/components/layout/Topbar.svelte';
  import EmptyState from '$lib/components/common/EmptyState.svelte';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Label } from '$lib/components/ui/label/index.js';
  import { Check, X } from 'lucide-svelte';

  let providers = $state<LdapProviderInfo[]>([]);
  let provider = $state('');
  let username = $state('');
  let password = $state('');
  let loading = $state(false);
  let loadError = $state('');
  let result = $state<LdapDiagnoseResult | null>(null);
  let testingConn = $state(false);
  let conn = $state<LdapConnectionResult | null>(null);

  onMount(() => {
    listLdapProviders()
      .then((res) => {
        providers = res.providers;
        if (providers.length > 0) provider = providers[0].name;
      })
      .catch((err) => (loadError = err.message ?? 'Failed to load providers'));
  });

  async function run(e: Event) {
    e.preventDefault();
    loading = true;
    result = null;
    loadError = '';
    try {
      result = await diagnoseLdap(provider, username, password);
    } catch (err: any) {
      loadError = err.message ?? 'Diagnostics request failed';
    } finally {
      loading = false;
    }
  }

  async function runConnection() {
    testingConn = true;
    conn = null;
    loadError = '';
    try {
      conn = await testLdapConnection(provider);
    } catch (err: any) {
      loadError = err.message ?? 'Connection test failed';
    } finally {
      testingConn = false;
    }
  }

  // Human-readable label for the resolution outcome.
  const outcomeLabels: Record<string, string> = {
    login_existing_identity: 'Log in — identity already linked to a user',
    link_existing_user: 'Link to an existing user with the same email',
    provision_new_user: 'Provision a new user account',
    rejected_provisioning_disabled: 'Rejected — no matching user and auto-provision is disabled',
    rejected_no_email: 'Rejected — directory entry has no email to link or provision on',
    rejected_not_in_login_group: 'Rejected — user is not a member of any allowed login group',
    error: 'Error during resolution lookup',
  };
</script>

<div class="flex flex-col h-full">
  <Topbar title="LDAP Diagnostics" />

  <div class="flex-1 overflow-auto p-6 space-y-6">
    {#if providers.length === 0}
      <EmptyState
        title="No LDAP providers configured"
        description="Add an ldap: block to config.yaml and restart partrack to enable directory login and these diagnostics."
      />
    {:else}
      <p class="text-sm text-muted-foreground max-w-2xl">
        Test a directory login end to end without affecting any account. This runs the real
        search-then-bind and group evaluation against the configured directory and reports exactly
        what a login <em>would</em> do — it never creates a user, identity, membership, or session.
      </p>

      <form onsubmit={run} class="space-y-4 max-w-md rounded-lg border border-border bg-card p-4">
        {#if providers.length > 1}
          <div class="space-y-1">
            <Label for="provider">Provider</Label>
            <select
              id="provider"
              bind:value={provider}
              class="w-full rounded-md border border-input bg-background px-3 py-2 text-sm outline-none focus:ring-2 focus:ring-ring"
            >
              {#each providers as p (p.name)}
                <option value={p.name}>{p.display_name} — {p.url}</option>
              {/each}
            </select>
          </div>
        {/if}

        <div class="space-y-1">
          <Label for="username">Username</Label>
          <Input id="username" bind:value={username} autocomplete="off" required />
        </div>
        <div class="space-y-1">
          <Label for="password">Password</Label>
          <Input id="password" type="password" bind:value={password} autocomplete="off" required />
        </div>

        <div class="flex items-center gap-2">
          <Button type="submit" disabled={loading || !username || !password}>
            {loading ? 'Testing…' : 'Run login test'}
          </Button>
          <Button type="button" variant="outline" disabled={testingConn} onclick={runConnection}>
            {testingConn ? 'Checking…' : 'Test connection'}
          </Button>
        </div>
        <p class="text-xs text-muted-foreground">
          “Test connection” checks reachability, TLS, the service bind and configured DNs — no user credentials needed.
        </p>
      </form>

      {#if loadError}
        <div class="rounded-md bg-destructive/10 border border-destructive/30 px-3 py-2 text-sm text-destructive">
          {loadError}
        </div>
      {/if}

      {#if conn}
        <!-- Connection test result -->
        <div
          class="rounded-md border px-4 py-3 text-sm font-medium
            {conn.connected
            ? 'border-green-600/30 bg-green-600/10 text-green-700 dark:text-green-400'
            : 'border-destructive/30 bg-destructive/10 text-destructive'}"
        >
          {conn.connected ? 'Directory reachable and service bind OK' : `Connection failed${conn.error ? ` — ${conn.error}` : ''}`}
        </div>

        <section class="space-y-2">
          <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Connection trace</h3>
          <ul class="space-y-1.5">
            {#each conn.steps as step (step.name)}
              <li class="flex items-start gap-2 text-sm">
                {#if step.ok}<Check class="mt-0.5 h-4 w-4 shrink-0 text-green-600" />{:else}<X class="mt-0.5 h-4 w-4 shrink-0 text-destructive" />{/if}
                <div>
                  <span class="font-medium">{step.name}</span>
                  {#if step.detail}<span class="text-muted-foreground"> — {step.detail}</span>{/if}
                  {#if step.err}<div class="text-destructive">{step.err}</div>{/if}
                </div>
              </li>
            {/each}
          </ul>
        </section>

        {#if conn.dn_checks && conn.dn_checks.length > 0}
          <section class="space-y-2">
            <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Configured DNs</h3>
            <ul class="space-y-1">
              {#each conn.dn_checks as c (c.dn)}
                <li class="flex items-start gap-2 text-sm">
                  {#if c.exists}<Check class="mt-0.5 h-4 w-4 shrink-0 text-green-600" />{:else}<X class="mt-0.5 h-4 w-4 shrink-0 text-destructive" />{/if}
                  <span class="font-mono text-xs break-all">{c.dn}</span>
                  {#if c.err}<span class="text-destructive text-xs">({c.err})</span>{/if}
                </li>
              {/each}
            </ul>
          </section>
        {/if}

        <details class="text-sm">
          <summary class="cursor-pointer text-muted-foreground">Effective configuration (password redacted)</summary>
          <pre class="mt-2 overflow-auto rounded-md border border-border bg-muted/40 p-3 text-xs">{JSON.stringify(conn.config, null, 2)}</pre>
        </details>
      {/if}

      {#if result}
        <!-- Overall outcome -->
        <div
          class="rounded-md border px-4 py-3 text-sm font-medium
            {result.authenticated
            ? 'border-green-600/30 bg-green-600/10 text-green-700 dark:text-green-400'
            : 'border-destructive/30 bg-destructive/10 text-destructive'}"
        >
          {result.authenticated ? 'Authentication succeeded' : `Authentication failed${result.error ? ` — ${result.error}` : ''}`}
        </div>

        {#if result.warnings && result.warnings.length > 0}
          <div class="rounded-md border border-amber-500/30 bg-amber-500/10 px-4 py-3 text-sm text-amber-700 dark:text-amber-400">
            <p class="font-medium">Warnings</p>
            <ul class="mt-1 list-disc pl-5 space-y-0.5">
              {#each result.warnings as wmsg (wmsg)}<li>{wmsg}</li>{/each}
            </ul>
          </div>
        {/if}

        <!-- Steps -->
        <section class="space-y-2">
          <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Bind trace</h3>
          <ul class="space-y-1.5">
            {#each result.steps as step (step.name)}
              <li class="flex items-start gap-2 text-sm">
                {#if step.ok}
                  <Check class="mt-0.5 h-4 w-4 shrink-0 text-green-600" />
                {:else}
                  <X class="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
                {/if}
                <div>
                  <span class="font-medium">{step.name}</span>
                  {#if step.detail}<span class="text-muted-foreground"> — {step.detail}</span>{/if}
                  {#if step.err}<div class="text-destructive">{step.err}</div>{/if}
                </div>
              </li>
            {/each}
          </ul>
        </section>

        {#if result.identity}
          <!-- Identity -->
          <section class="space-y-2">
            <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Resolved identity</h3>
            <dl class="grid grid-cols-[max-content_1fr] gap-x-4 gap-y-1 text-sm">
              <dt class="text-muted-foreground">DN</dt><dd class="font-mono text-xs break-all">{result.identity.dn}</dd>
              <dt class="text-muted-foreground">Subject</dt><dd class="font-mono text-xs break-all">{result.identity.subject}</dd>
              <dt class="text-muted-foreground">Email</dt><dd>{result.identity.email || '—'}</dd>
              <dt class="text-muted-foreground">Display name</dt><dd>{result.identity.display_name || '—'}</dd>
              <dt class="text-muted-foreground">Groups</dt>
              <dd>
                {#if (result.identity.groups ?? []).length === 0}—{:else}
                  <ul class="space-y-0.5">
                    {#each result.identity.groups ?? [] as g (g)}<li class="font-mono text-xs break-all">{g}</li>{/each}
                  </ul>
                {/if}
              </dd>
            </dl>
          </section>
        {/if}

        {#if result.authorization}
          <!-- Authorization -->
          <section class="space-y-2">
            <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Authorization</h3>
            {#if result.authorization.required_group_configured}
              <p class="text-sm">
                Login gate:
                <span class="font-medium {result.authorization.login_allowed ? 'text-green-600' : 'text-destructive'}">
                  {result.authorization.login_allowed ? 'allowed (member of required group)' : 'denied (not in required group)'}
                </span>
              </p>
            {/if}
            {#if result.authorization.admin_group_configured}
              <p class="text-sm">
                Instance admin:
                <span class="font-medium {result.authorization.is_admin ? 'text-green-600' : ''}">
                  {result.authorization.is_admin ? 'granted (member of admin group)' : 'not granted'}
                </span>
              </p>
            {:else}
              <p class="text-sm text-muted-foreground">No admin group configured — instance admin flag is not directory-controlled.</p>
            {/if}

            {#if (result.authorization.mappings ?? []).length > 0}
              <table class="text-sm w-full max-w-3xl">
                <thead class="text-muted-foreground text-left">
                  <tr><th class="py-1 pr-4 font-medium">Group</th><th class="py-1 pr-4 font-medium">Team</th><th class="py-1 pr-4 font-medium">Role</th><th class="py-1 font-medium">Matched</th></tr>
                </thead>
                <tbody>
                  {#each result.authorization.mappings ?? [] as m (m.group + m.team + m.role)}
                    <tr class="border-t border-border">
                      <td class="py-1 pr-4 font-mono text-xs break-all">{m.group || '(all users)'}</td>
                      <td class="py-1 pr-4">{m.team}</td>
                      <td class="py-1 pr-4">
                        {m.role}{#if !m.valid_role}<span class="text-destructive"> (invalid)</span>{/if}
                      </td>
                      <td class="py-1">
                        {#if m.matched}<Check class="h-4 w-4 text-green-600" />{:else}<X class="h-4 w-4 text-muted-foreground" />{/if}
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            {/if}
          </section>
        {/if}

        {#if (result.group_checks ?? []).length > 0}
          <!-- Group checks: does each referenced group exist, and is the user in it? -->
          <section class="space-y-2">
            <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Group checks</h3>
            <p class="text-xs text-muted-foreground max-w-3xl">
              Every group partrack references — the allowed login group(s), the admin group, and each team
              mapping — with whether it exists in the directory and whether this user is a member. Membership
              counts nested groups.
            </p>
            <table class="text-sm w-full max-w-3xl">
              <thead class="text-muted-foreground text-left">
                <tr>
                  <th class="py-1 pr-4 font-medium">Group</th>
                  <th class="py-1 pr-4 font-medium">For</th>
                  <th class="py-1 pr-4 font-medium">Exists</th>
                  <th class="py-1 font-medium">Member</th>
                </tr>
              </thead>
              <tbody>
                {#each result.group_checks ?? [] as g (g.dn)}
                  <tr class="border-t border-border align-top">
                    <td class="py-1 pr-4 font-mono text-xs break-all">{g.dn}</td>
                    <td class="py-1 pr-4 text-muted-foreground">{(g.purposes ?? []).join(', ')}</td>
                    <td class="py-1 pr-4">
                      {#if g.exists}<Check class="h-4 w-4 text-green-600" />{:else}
                        <span class="flex items-center gap-1 text-destructive">
                          <X class="h-4 w-4" />{#if g.exists_err}<span class="text-xs">{g.exists_err}</span>{/if}
                        </span>
                      {/if}
                    </td>
                    <td class="py-1">
                      {#if g.member}
                        <span class="flex items-center gap-1">
                          <Check class="h-4 w-4 text-green-600" />
                          {#if g.member_via}<span class="text-xs text-muted-foreground">({g.member_via})</span>{/if}
                        </span>
                      {:else}<X class="h-4 w-4 text-muted-foreground" />{/if}
                    </td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </section>
        {/if}

        {#if result.resolution}
          <!-- Resolution -->
          <section class="space-y-2">
            <h3 class="text-sm font-semibold text-muted-foreground uppercase tracking-wide">Account resolution</h3>
            <p class="text-sm">{outcomeLabels[result.resolution.outcome] ?? result.resolution.outcome}</p>
            {#if result.resolution.linked_user_email}
              <p class="text-sm text-muted-foreground">Linked user: {result.resolution.linked_user_email}</p>
            {/if}
            {#if result.resolution.existing_user_display_name}
              <p class="text-sm text-muted-foreground">Existing user by email: {result.resolution.existing_user_display_name}</p>
            {/if}

            {#if (result.resolution.teams ?? []).length > 0}
              <p class="text-xs font-medium text-muted-foreground pt-1">Team sync (what login would do)</p>
              <table class="text-sm w-full max-w-3xl">
                <thead class="text-muted-foreground text-left">
                  <tr><th class="py-1 pr-4 font-medium">Team</th><th class="py-1 pr-4 font-medium">Action</th><th class="py-1 font-medium">Current</th></tr>
                </thead>
                <tbody>
                  {#each result.resolution.teams ?? [] as t (t.team)}
                    <tr class="border-t border-border">
                      <td class="py-1 pr-4">{t.team}{#if !t.exists}<span class="text-destructive"> (missing)</span>{/if}</td>
                      <td class="py-1 pr-4">{t.action}</td>
                      <td class="py-1 text-muted-foreground">
                        {#if t.current_role}{t.current_role}{t.current_directory_managed === false ? ' (manual)' : ''}{:else}—{/if}
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            {/if}

            {#if (result.resolution.team_memberships ?? []).length > 0}
              <p class="text-xs font-medium text-muted-foreground pt-1">Current team memberships</p>
              <table class="text-sm w-full max-w-3xl">
                <thead class="text-muted-foreground text-left">
                  <tr><th class="py-1 pr-4 font-medium">Team</th><th class="py-1 pr-4 font-medium">Role</th><th class="py-1 font-medium">Source</th></tr>
                </thead>
                <tbody>
                  {#each result.resolution.team_memberships ?? [] as m (m.team)}
                    <tr class="border-t border-border">
                      <td class="py-1 pr-4">{m.team}</td>
                      <td class="py-1 pr-4">{m.role}</td>
                      <td class="py-1 text-muted-foreground">{m.directory_managed ? 'directory-managed' : 'manual'}</td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            {/if}
          </section>
        {/if}
      {/if}
    {/if}
  </div>
</div>
