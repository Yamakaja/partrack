<script lang="ts">
  import CameraViewport from './CameraViewport.svelte';
  import type { Snippet } from 'svelte';

  let { onBack, onScan, label, topRightControls }: {
    onBack: () => void;
    onScan: (raw: string) => Promise<void>;
    label?: string | null;
    topRightControls?: Snippet;
  } = $props();
</script>

<div class="relative flex-1 overflow-hidden">
  <CameraViewport {onScan} {topRightControls} />

  <button
    onclick={onBack}
    aria-label="Back"
    class="absolute top-3 left-3 z-10 flex h-8 w-8 items-center justify-center rounded-full bg-black/50 text-white"
  >
    <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
    </svg>
  </button>

  {#if label}
    <div class="pointer-events-none absolute top-3 left-0 right-0 z-10 text-center">
      <span class="inline-block max-w-[55%] truncate rounded-full bg-black/60 px-3 py-1 text-xs text-zinc-300">{label}</span>
    </div>
  {/if}
</div>
