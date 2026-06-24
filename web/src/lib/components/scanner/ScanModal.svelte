<script lang="ts">
  import { onDestroy } from 'svelte';
  import { Dialog as DialogPrimitive } from 'bits-ui';
  import { Button } from '$lib/components/ui/button/index.js';
  import { request } from '$lib/api/client.js';
  import { subscribeSSEType } from '$lib/api/sse.js';
  import { setScanSession, clearScanSession } from '$lib/stores/scanSession.svelte.js';
  import QRCode from 'qrcode';

  let {
    teamId,
    orderId = '',
    open = $bindable(false),
    onOpenChange,
  }: {
    teamId: string;
    orderId?: string;
    open?: boolean;
    onOpenChange?: (v: boolean) => void;
  } = $props();

  // ── State ────────────────────────────────────────────────────────────────

  type ModalState = 'idle' | 'checking' | 'loading' | 'waiting' | 'pushing_context' | 'connected';

  let modalState = $state<ModalState>('idle');
  let sessionId = $state('');
  let qrSvg = $state('');
  let unsubSSE: (() => void) | null = null;
  let pairingRefresh: ReturnType<typeof setInterval> | null = null;

  // renderPairingQR encodes the pairing ticket in the URL fragment (#pt=…) so the
  // ticket is never sent to the server (kept out of access logs / Referer).
  async function renderPairingQR(ticket: string) {
    const qrUrl = `${window.location.origin}/scan#pt=${encodeURIComponent(ticket)}`;
    qrSvg = await QRCode.toString(qrUrl, { type: 'svg', errorCorrectionLevel: 'M', margin: 1 });
  }

  // ── Session management ───────────────────────────────────────────────────

  async function start() {
    modalState = 'checking';
    try {
      // Check for existing active session. Returns undefined on 204 (no session).
      const existing = await request<{
        id: string;
        phone_connected: boolean;
      } | undefined>('GET', `/api/v1/teams/${teamId}/scan-sessions/active`);

      if (!existing) {
        // No session — create fresh.
        await createSession();
        return;
      }

      if (existing.phone_connected) {
        if (orderId) {
          // Phone is live and we have an order to push.
          await pushContext(existing.id);
        } else {
          // Already connected, nothing to do — just close.
          onPhoneConnected();
        }
        return;
      }

      // Session exists but phone is not live — delete it and start fresh.
      try {
        await request('DELETE', `/api/v1/teams/${teamId}/scan-sessions/${existing.id}`);
      } catch {}
      clearScanSession();
      await createSession();
    } catch {
      modalState = 'idle';
    }
  }

  async function createSession() {
    modalState = 'loading';
    try {
      const data = await request<{ id: string; pairing_ticket: string; expires_at: string }>(
        'POST',
        `/api/v1/teams/${teamId}/scan-sessions`,
      );
      sessionId = data.id;
      await renderPairingQR(data.pairing_ticket);
      startPairingRefresh();

      setScanSession({
        id: data.id,
        teamId,
        token: '',
        connected: false,
        liveConnected: false,
      });

      // Subscribe to scan_session_changed SSE to detect phone connect.
      // If an orderId was passed, push context immediately after phone connects.
      unsubSSE = subscribeSSEType(teamId, 'scan_session_changed', (ev) => {
        if (ev.data.session_id === sessionId && ev.data.phone_connected) {
          if (orderId) {
            pushContext(sessionId);
          } else {
            onPhoneConnected();
          }
        }
      });

      modalState = 'waiting';
    } catch {
      modalState = 'idle';
    }
  }

  // startPairingRefresh re-mints the pairing ticket before it expires (TTL 60s)
  // so the displayed QR stays valid; overlapping windows mean no pairing gap.
  function startPairingRefresh() {
    stopPairingRefresh();
    pairingRefresh = setInterval(async () => {
      if (!sessionId) return;
      try {
        const res = await request<{ pairing_ticket: string }>(
          'POST',
          `/api/v1/teams/${teamId}/scan-sessions/${sessionId}/pairing-ticket`,
        );
        await renderPairingQR(res.pairing_ticket);
      } catch {
        // transient; keep the current QR and try again next tick
      }
    }, 30000);
  }

  function stopPairingRefresh() {
    if (pairingRefresh) { clearInterval(pairingRefresh); pairingRefresh = null; }
  }

  async function pushContext(existingSessionId: string) {
    modalState = 'pushing_context';
    try {
      await request(
        'PATCH',
        `/api/v1/teams/${teamId}/scan-sessions/${existingSessionId}/context`,
        { order_id: orderId },
      );
      onPhoneConnected();
    } catch {
      modalState = 'idle';
    }
  }

  function onPhoneConnected() {
    modalState = 'connected';
    setTimeout(() => closeModal(true), 800);
  }

  function closeModal(connected = false) {
    if (!connected && sessionId) {
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
    stopPairingRefresh();
    sessionId = '';
    qrSvg = '';
    modalState = 'idle';
  }

  // ── Lifecycle ────────────────────────────────────────────────────────────

  $effect(() => {
    if (open && modalState === 'idle') {
      start();
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

      <!-- Checking / loading spinner -->
      {#if modalState === 'checking' || modalState === 'loading'}
        <div class="flex items-center justify-center py-12">
          <div class="h-6 w-6 animate-spin rounded-full border-2 border-border border-t-foreground"></div>
        </div>

      <!-- Pushing context to already-connected phone -->
      {:else if modalState === 'pushing_context'}
        <div class="flex flex-col items-center gap-3 py-8 text-center">
          <div class="h-6 w-6 animate-spin rounded-full border-2 border-border border-t-foreground"></div>
          <p class="text-sm text-muted-foreground">Sending order to phone…</p>
        </div>

      <!-- Waiting for phone -->
      {:else if modalState === 'waiting'}
        <div class="space-y-4">
          <p class="text-sm text-muted-foreground text-center">
            Scan this QR code with your phone to connect.
          </p>
          <div class="flex justify-center rounded-lg border border-border bg-white p-3">
            <!-- eslint-disable-next-line svelte/no-at-html-tags -->
            {@html qrSvg}
          </div>
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
          <div class="flex h-12 w-12 items-center justify-center rounded-full bg-success-subtle text-success">
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
