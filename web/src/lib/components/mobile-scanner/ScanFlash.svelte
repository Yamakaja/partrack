<script lang="ts">
  import { onMount } from 'svelte';
  import { scanPulse, installAudioUnlock, type ScanOutcome } from './feedback.svelte.js';

  // Full-screen flash for every scan verdict (see feedback.svelte.ts). Mounted
  // once at the scanner root so it covers every screen — including the ones a
  // scan navigates away from the camera. Keyed on the pulse sequence, so two
  // identical verdicts in a row still flash twice. Never intercepts touches.

  onMount(installAudioUnlock);

  const STYLE: Record<ScanOutcome, { ring: string; tint: string; badge: string; label: string }> = {
    success:  { ring: '#10b981', tint: 'rgba(16,185,129,0.22)', badge: 'bg-emerald-500', label: 'OK' },
    complete: { ring: '#10b981', tint: 'rgba(16,185,129,0.35)', badge: 'bg-emerald-500', label: 'Done' },
    notice:   { ring: '#38bdf8', tint: 'rgba(56,189,248,0.16)', badge: 'bg-sky-500', label: 'Again' },
    warning:  { ring: '#f59e0b', tint: 'rgba(245,158,11,0.22)', badge: 'bg-amber-500', label: 'Check' },
    error:    { ring: '#ef4444', tint: 'rgba(239,68,68,0.26)', badge: 'bg-red-500', label: 'No' },
  };

  const style = $derived(scanPulse.outcome ? STYLE[scanPulse.outcome] : null);
</script>

{#if style && scanPulse.seq > 0}
  {#key scanPulse.seq}
    <div
      class="scan-flash pointer-events-none fixed inset-0 z-[70]"
      style="--ring: {style.ring}; --tint: {style.tint}"
      aria-hidden="true"
    ></div>
    <div class="pointer-events-none fixed inset-0 z-[71] flex items-center justify-center" aria-hidden="true">
      <div class="scan-badge flex h-24 w-24 flex-col items-center justify-center rounded-full text-white shadow-2xl {style.badge}">
        {#if scanPulse.outcome === 'success' || scanPulse.outcome === 'complete'}
          <svg class="h-12 w-12" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M5 13l4 4L19 7" />
          </svg>
        {:else if scanPulse.outcome === 'notice'}
          <svg class="h-11 w-11" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5"
              d="M16.023 9.348h4.992V4.356M2.985 19.644v-4.992h4.992m0 0 3.181 3.183a8.25 8.25 0 0 0 13.803-3.7M4.031 9.865a8.25 8.25 0 0 1 13.803-3.7l3.181 3.182" />
          </svg>
        {:else if scanPulse.outcome === 'warning'}
          <span class="text-5xl font-black leading-none">!</span>
        {:else}
          <svg class="h-12 w-12" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="3" d="M6 18L18 6M6 6l12 12" />
          </svg>
        {/if}
        <span class="mt-0.5 text-[10px] font-bold uppercase tracking-wider">{style.label}</span>
      </div>
    </div>
  {/key}
{/if}

<style>
  .scan-flash {
    box-shadow: inset 0 0 0 10px var(--ring);
    background: var(--tint);
    opacity: 0;
    animation: scan-flash 650ms ease-out;
  }
  @keyframes scan-flash {
    0%   { opacity: 1; }
    40%  { opacity: 1; }
    100% { opacity: 0; }
  }
  .scan-badge {
    opacity: 0;
    animation: scan-badge 750ms cubic-bezier(0.2, 0.9, 0.3, 1.2);
  }
  @keyframes scan-badge {
    0%   { opacity: 0; transform: scale(0.5); }
    25%  { opacity: 1; transform: scale(1.08); }
    40%  { transform: scale(1); }
    75%  { opacity: 1; }
    100% { opacity: 0; transform: scale(1); }
  }
  @keyframes scan-badge-still {
    0%, 75% { opacity: 1; }
    100%    { opacity: 0; }
  }
  @media (prefers-reduced-motion: reduce) {
    .scan-badge { animation-name: scan-badge-still; }
  }
</style>
