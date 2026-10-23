<script lang="ts">
  import { createScanner, type Scanner } from './scanner.svelte.js';
  import CameraSelector from './CameraSelector.svelte';
  import { scanPulse, scanFeedbackPrefs } from './feedback.svelte.js';
  import { untrack, type Snippet } from 'svelte';

  // Shared live-camera viewport for every mobile-scanner screen: it owns the
  // scanner lifecycle (camera start/stop, hold-to-scan) and renders the <video>,
  // the camera-error overlay, the reticle, a CameraSelector, and a default
  // hold-to-scan button. Place it inside a `relative` sized container; its <video>
  // fills the box and everything else is layered over it.
  //
  // Consumers customise via snippets:
  //   topRightControls — extra controls next to the camera selector
  //   overlay          — arbitrary layer over the feed (toasts, error banners)
  //   bottomBar        — replaces the default hold-to-scan; receives the scanner
  //                      so the consumer can wire its own hold-to-scan button
  let {
    onScan,
    reticleSize = 'lg',
    scanButtonSize = 'lg',
    topRightControls,
    overlay,
    bottomBar,
  }: {
    onScan: (raw: string) => Promise<void>;
    reticleSize?: 'sm' | 'lg';
    scanButtonSize?: 'sm' | 'lg';
    topRightControls?: Snippet;
    overlay?: Snippet;
    bottomBar?: Snippet<[Scanner]>;
  } = $props();

  let videoEl = $state<HTMLVideoElement | null>(null);
  const scanner = createScanner(() => videoEl);
  // Decodes from before this viewport mounted must not replay the reticle pulse.
  const mountDecodeSeq = untrack(() => scanPulse.decodeSeq);

  $effect(() => {
    const deviceId = scanner.selectedCameraId;
    if (videoEl) {
      scanner.startCamera(deviceId);
      return () => scanner.stopCamera();
    }
  });
</script>

<!-- svelte-ignore a11y_media_has_caption -->
<video bind:this={videoEl} autoplay playsinline muted class="h-full w-full object-cover"></video>

<div class="absolute top-3 right-3 z-10 flex items-center gap-2">
  {@render topRightControls?.()}
  <button
    onclick={() => (scanFeedbackPrefs.sound = !scanFeedbackPrefs.sound)}
    aria-label={scanFeedbackPrefs.sound ? 'Mute scan sounds' : 'Unmute scan sounds'}
    aria-pressed={scanFeedbackPrefs.sound}
    class="flex h-8 w-8 items-center justify-center rounded-full bg-black/50 {scanFeedbackPrefs.sound ? 'text-white' : 'text-zinc-500'}"
  >
    <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      {#if scanFeedbackPrefs.sound}
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
          d="M19.114 5.636a9 9 0 0 1 0 12.728M16.463 8.288a5.25 5.25 0 0 1 0 7.424M6.75 8.25l4.72-4.72a.75.75 0 0 1 1.28.53v15.88a.75.75 0 0 1-1.28.53l-4.72-4.72H4.51c-.88 0-1.704-.507-1.938-1.354A9.009 9.009 0 0 1 2.25 12c0-.83.112-1.633.322-2.396C2.806 8.756 3.63 8.25 4.51 8.25H6.75Z" />
      {:else}
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
          d="M17.25 9.75 19.5 12m0 0 2.25 2.25M19.5 12l2.25-2.25M19.5 12l-2.25 2.25m-10.5-6 4.72-4.72a.75.75 0 0 1 1.28.53v15.88a.75.75 0 0 1-1.28.53l-4.72-4.72H4.51c-.88 0-1.704-.507-1.938-1.354A9.009 9.009 0 0 1 2.25 12c0-.83.112-1.633.322-2.396C2.806 8.756 3.63 8.25 4.51 8.25H6.75Z" />
      {/if}
    </svg>
  </button>
  <CameraSelector {scanner} />
</div>

{#if scanner.cameraError}
  <div class="absolute inset-0 flex flex-col items-center justify-center gap-4 bg-black/90 px-8 text-center">
    <svg class="h-12 w-12 text-zinc-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
        d="M15.75 10.5l4.72-4.72a.75.75 0 011.28.53v11.38a.75.75 0 01-1.28.53l-4.72-4.72M4.5 18.75h9a2.25 2.25 0 002.25-2.25v-9a2.25 2.25 0 00-2.25-2.25h-9A2.25 2.25 0 002.25 7.5v9a2.25 2.25 0 002.25 2.25z" />
      <line x1="3" y1="3" x2="21" y2="21" stroke-width="1.5" stroke-linecap="round" />
    </svg>
    <p class="text-sm text-zinc-300">{scanner.cameraError}</p>
  </div>
{:else}
  <div class="pointer-events-none absolute inset-0 flex items-center justify-center">
    <!-- Keyed on the decode counter: the reticle snaps the instant a code is
         read, before the server's verdict arrives (feedback.svelte.ts). -->
    <div class="rounded-lg shadow-[0_0_0_9999px_rgba(0,0,0,0.4)] {scanner.isScanning ? 'reticle-hunting' : ''}">
      {#key scanPulse.decodeSeq}
        <div class="{reticleSize === 'sm' ? 'h-36 w-36' : 'h-56 w-56'} rounded-lg border-2
          {scanner.isScanning ? 'border-white' : 'border-white/60'}
          {scanPulse.decodeSeq > mountDecodeSeq ? 'reticle-hit' : ''}"></div>
      {/key}
    </div>
  </div>

  {@render overlay?.()}

  {#if bottomBar}
    {@render bottomBar(scanner)}
  {:else}
    <div class="absolute bottom-0 left-0 right-0 flex justify-center {scanButtonSize === 'sm' ? 'pb-4' : 'pb-14'}">
      <button
        onpointerdown={(e) => { e.currentTarget.setPointerCapture(e.pointerId); scanner.startContinuousScan(onScan); }}
        onpointerup={() => scanner.stopContinuousScan()}
        onpointercancel={() => scanner.stopContinuousScan()}
        style="touch-action: none"
        class="rounded-full font-semibold transition-colors {scanButtonSize === 'sm' ? 'px-10 py-3.5' : 'px-14 py-5 text-lg'} {scanner.isScanning ? 'bg-zinc-200 text-black' : 'bg-white text-black active:bg-zinc-200'}"
      >
        {scanner.isScanning ? 'Scanning…' : 'Hold to scan'}
      </button>
    </div>
  {/if}
{/if}

<style>
  /* Hold-to-scan in progress: the reticle breathes so "is it looking?" is
     never a question. */
  .reticle-hunting { animation: reticle-hunt 900ms ease-in-out infinite; }
  @keyframes reticle-hunt {
    0%, 100% { transform: scale(1); }
    50%      { transform: scale(0.96); }
  }
  /* A code was decoded: a white snap-in pulse. */
  .reticle-hit { animation: reticle-hit 350ms ease-out; }
  @keyframes reticle-hit {
    0%   { transform: scale(1.12); border-width: 6px; border-color: #fff; }
    100% { transform: scale(1); }
  }
  @media (prefers-reduced-motion: reduce) {
    .reticle-hunting, .reticle-hit { animation: none; }
  }
</style>
