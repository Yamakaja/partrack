<script lang="ts">
  import { createScanner, type Scanner } from './scanner.svelte.js';
  import CameraSelector from './CameraSelector.svelte';
  import type { Snippet } from 'svelte';

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
    <div class="{reticleSize === 'sm' ? 'h-36 w-36' : 'h-56 w-56'} rounded-lg border-2 border-white/60 shadow-[0_0_0_9999px_rgba(0,0,0,0.4)]"></div>
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
