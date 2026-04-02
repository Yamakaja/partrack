<script lang="ts">
  import { onDestroy } from 'svelte';
  import { Dialog as DialogPrimitive } from 'bits-ui';
  import { Button } from '$lib/components/ui/button/index.js';
  import { request, ApiError } from '$lib/api/client.js';
  import { subscribeSSEType } from '$lib/api/sse.js';
  import { setScanSession, clearScanSession } from '$lib/stores/scanSession.svelte.js';
  import QRCode from 'qrcode';

  let {
    teamId,
    orderId,
    open = $bindable(false),
    onOpenChange,
  }: {
    teamId: string;
    orderId: string;
    open?: boolean;
    onOpenChange?: (v: boolean) => void;
  } = $props();

  // ── State ────────────────────────────────────────────────────────────────

  type ModalState = 'idle' | 'conflict' | 'loading' | 'waiting' | 'connected';

  let modalState = $state<ModalState>('idle');
  let sessionId = $state('');
  let qrSvg = $state('');
  let existingSessionId = $state('');
  let unsubSSE: (() => void) | null = null;

  // ── Session management ───────────────────────────────────────────────────

  async function createSession() {
    modalState = 'loading';
    try {
      const data = await request<{ id: string; token: string; expires_at: string }>(
        'POST',
        `/api/v1/teams/${teamId}/scan-sessions`,
        { order_id: orderId },
      );
      sessionId = data.id;
      const qrUrl = `${window.location.origin}/scan?token=${encodeURIComponent(data.token)}`;
      qrSvg = await QRCode.toString(qrUrl, { type: 'svg', errorCorrectionLevel: 'M', margin: 1 });

      // Record session in store so other components can read it.
      setScanSession({
        id: data.id,
        teamId,
        orderId,
        orderRef: '',  // will be updated on phone connect
        token: data.token,
        connected: false,
      });

      // Subscribe to scan_session_changed SSE to detect phone connect.
      unsubSSE = subscribeSSEType(teamId, 'scan_session_changed', (ev) => {
        if (ev.data.session_id === sessionId && ev.data.phone_connected) {
          onPhoneConnected();
        }
      });

      modalState = 'waiting';
    } catch (e) {
      if (e instanceof ApiError && e.code === 'SCAN_SESSION_EXISTS') {
        existingSessionId = (e.data.existing_session_id as string) ?? '';
        modalState = 'conflict';
      } else {
        modalState = 'idle';
      }
    }
  }

  async function replaceSession() {
    if (existingSessionId) {
      try {
        await request('DELETE', `/api/v1/teams/${teamId}/scan-sessions/${existingSessionId}`);
      } catch {}
      clearScanSession();
    }
    existingSessionId = '';
    await createSession();
  }

  function onPhoneConnected() {
    modalState = 'connected';
    // Auto-close the modal after a brief moment so the user sees the confirmation.
    setTimeout(() => {
      closeModal(true);
    }, 800);
  }

  function closeModal(connected = false) {
    if (!connected && sessionId) {
      // User closed manually — revoke the session.
      request('DELETE', `/api/v1/teams/${teamId}/scan-sessions/${sessionId}`).catch(() => {});
      clearScanSession();
    }
    cleanup();
    open = false;
    onOpenChange?.(false);
  }

  function cleanup() {
    unsubSSE?.();
    unsubSSE = null;
    sessionId = '';
    qrSvg = '';
    modalState = 'idle';
  }

  // ── Lifecycle ────────────────────────────────────────────────────────────

  $effect(() => {
    if (open && modalState === 'idle') {
      createSession();
    }
    if (!open && modalState !== 'idle') {
      cleanup();
    }
  });

  onDestroy(() => {
    cleanup();
  });
</script>

<DialogPrimitive.Root
  {open}
  onOpenChange={(v) => {
    if (!v) closeModal(false);
  }}
>
  <DialogPrimitive.Portal>
    <DialogPrimitive.Overlay
      class="fixed inset-0 z-50 bg-black/80 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0"
    />
    <DialogPrimitive.Content
      class="fixed left-1/2 top-1/2 z-50 w-full max-w-sm -translate-x-1/2 -translate-y-1/2 rounded-lg border border-border bg-background p-6 shadow-lg duration-200 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95"
    >
      <DialogPrimitive.Title class="text-lg font-semibold leading-none tracking-tight mb-4">
        Mobile Scanner
      </DialogPrimitive.Title>

      <!-- Conflict: existing session -->
      {#if modalState === 'conflict'}
        <div class="space-y-4">
          <p class="text-sm text-muted-foreground">
            You already have an active scanner session. Starting a new one will disconnect the previous phone.
          </p>
          <div class="flex justify-end gap-2">
            <Button variant="outline" onclick={() => closeModal(false)}>Cancel</Button>
            <Button variant="destructive" onclick={replaceSession}>Replace</Button>
          </div>
        </div>

      <!-- Loading QR -->
      {:else if modalState === 'loading'}
        <div class="flex items-center justify-center py-12">
          <div class="h-6 w-6 animate-spin rounded-full border-2 border-border border-t-foreground"></div>
        </div>

      <!-- Waiting for phone -->
      {:else if modalState === 'waiting'}
        <div class="space-y-4">
          <p class="text-sm text-muted-foreground text-center">
            Scan this QR code with your phone to start scanning parts.
          </p>
          <!-- QR code -->
          <div class="flex justify-center rounded-lg border border-border bg-white p-3">
            <!-- eslint-disable-next-line svelte/no-at-html-tags -->
            {@html qrSvg}
          </div>
          <!-- Status badge -->
          <div class="flex items-center justify-center gap-2 text-sm text-muted-foreground">
            <span class="inline-block h-2 w-2 animate-pulse rounded-full bg-zinc-400"></span>
            Waiting for phone…
          </div>
          <div class="flex justify-end">
            <Button variant="outline" onclick={() => closeModal(false)}>Cancel</Button>
          </div>
        </div>

      <!-- Phone connected — auto-closes -->
      {:else if modalState === 'connected'}
        <div class="flex flex-col items-center gap-3 py-6">
          <div class="flex h-12 w-12 items-center justify-center rounded-full bg-green-100 text-green-600 dark:bg-green-900/30">
            <svg class="h-6 w-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
            </svg>
          </div>
          <p class="text-sm font-medium">Phone connected!</p>
        </div>
      {/if}

    </DialogPrimitive.Content>
  </DialogPrimitive.Portal>
</DialogPrimitive.Root>
