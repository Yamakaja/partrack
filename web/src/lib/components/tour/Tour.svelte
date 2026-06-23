<script lang="ts">
  import {
    tourStore,
    STEPS,
    nextStep,
    prevStep,
    skipTour,
    finishTour,
    finishAndOpenGuide,
  } from '$lib/stores/tour.svelte.js';

  let step = $derived(STEPS[tourStore.index]);
  let isFirst = $derived(tourStore.index === 0);
  let isLast = $derived(tourStore.index === STEPS.length - 1);
  // Targetless steps (welcome / wrap-up) render as a centered card and read
  // better with more room than the spotlight-anchored steps.
  let centered = $derived(!step?.target);

  const PAD = 6; // spotlight padding around the target
  const GAP = 12; // card gap from the target

  // Viewport-coordinate rect of the current spotlight target (null = centered).
  let rect = $state<{ top: number; left: number; width: number; height: number } | null>(null);
  let cardEl = $state<HTMLDivElement | null>(null);
  let cardPos = $state<{ top: number; left: number }>({
    top: Math.round(window.innerHeight / 2),
    left: Math.round(window.innerWidth / 2),
  });

  function measure() {
    const sel = step?.target;
    if (!tourStore.active || !sel) {
      rect = null;
      return;
    }
    const el = document.querySelector(sel) as HTMLElement | null;
    if (!el) {
      rect = null; // target missing (e.g. role-hidden) → no spotlight for this step
      return;
    }
    el.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    const r = el.getBoundingClientRect();
    rect = { top: r.top, left: r.left, width: r.width, height: r.height };
  }

  // Re-measure on step change and when the viewport moves.
  $effect(() => {
    if (!tourStore.active) return;
    void tourStore.index;
    measure();
    const onMove = () => measure();
    window.addEventListener('resize', onMove);
    window.addEventListener('scroll', onMove, true);
    return () => {
      window.removeEventListener('resize', onMove);
      window.removeEventListener('scroll', onMove, true);
    };
  });

  // Position the card next to the spotlight (or centered when there's no target).
  $effect(() => {
    const r = rect;
    const card = cardEl;
    void step; // reposition when the step (side) changes
    if (!card) return;
    const cw = card.offsetWidth;
    const ch = card.offsetHeight;
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const M = 8;

    if (!r) {
      cardPos = { top: Math.round((vh - ch) / 2), left: Math.round((vw - cw) / 2) };
      return;
    }

    let top: number;
    let left: number;
    switch (step?.side) {
      case 'bottom':
        top = r.top + r.height + GAP;
        left = r.left;
        break;
      case 'top':
        top = r.top - ch - GAP;
        left = r.left;
        break;
      case 'left':
        left = r.left - cw - GAP;
        top = r.top;
        break;
      default: // 'right'
        left = r.left + r.width + GAP;
        top = r.top;
        break;
    }
    top = Math.min(Math.max(top, M), vh - ch - M);
    left = Math.min(Math.max(left, M), vw - cw - M);
    cardPos = { top: Math.round(top), left: Math.round(left) };
  });

  function onKey(e: KeyboardEvent) {
    if (!tourStore.active) return;
    if (e.key === 'Escape') skipTour();
    else if (e.key === 'ArrowRight' || e.key === 'Enter') nextStep();
    else if (e.key === 'ArrowLeft') prevStep();
  }
</script>

<svelte:window onkeydown={onKey} />

{#if tourStore.active}
  <!-- Transparent click-blocker: swallows interaction with the app while the
       tour runs. The dim itself comes from the spotlight box-shadow above it. -->
  <div class="fixed inset-0 z-[60]" aria-hidden="true"></div>

  {#if rect}
    <div
      class="pointer-events-none fixed z-[61] rounded-lg transition-all duration-200"
      style="top:{rect.top - PAD}px; left:{rect.left - PAD}px; width:{rect.width +
        PAD * 2}px; height:{rect.height + PAD * 2}px;
        box-shadow: 0 0 0 2px rgba(255,255,255,0.75), 0 0 0 9999px rgba(0,0,0,0.55);"
    ></div>
  {:else}
    <div class="fixed inset-0 z-[61] bg-black/55" aria-hidden="true"></div>
  {/if}

  <div
    bind:this={cardEl}
    role="dialog"
    aria-modal="true"
    aria-label={step.title}
    class="fixed z-[62] max-w-[calc(100vw-1rem)] rounded-lg border border-border bg-background shadow-xl {centered
      ? 'w-[28rem] p-6'
      : 'w-80 p-4'}"
    style="top:{cardPos.top}px; left:{cardPos.left}px;"
  >
    <h2 class="text-sm font-semibold">{step.title}</h2>
    <p class="mt-1.5 text-sm leading-relaxed text-muted-foreground">{step.body}</p>

    <div class="mt-4 flex items-center justify-between gap-2">
      <span class="text-xs tabular-nums text-muted-foreground">{tourStore.index + 1} / {STEPS.length}</span>
      <div class="flex items-center gap-2">
        {#if !isLast}
          <button onclick={skipTour} class="text-xs text-muted-foreground hover:text-foreground">Skip</button>
        {/if}
        {#if !isFirst && !isLast}
          <button
            onclick={prevStep}
            class="rounded-md border border-border px-2.5 py-1 text-xs font-medium hover:bg-accent/50"
          >
            Back
          </button>
        {/if}
        {#if isLast}
          <button
            onclick={finishTour}
            class="rounded-md border border-border px-2.5 py-1 text-xs font-medium hover:bg-accent/50"
          >
            Done
          </button>
          <button
            onclick={finishAndOpenGuide}
            class="rounded-md bg-primary px-2.5 py-1 text-xs font-medium text-primary-foreground hover:bg-primary/90"
          >
            Open Getting started
          </button>
        {:else}
          <button
            onclick={nextStep}
            class="rounded-md bg-primary px-2.5 py-1 text-xs font-medium text-primary-foreground hover:bg-primary/90"
          >
            {isFirst ? 'Start' : 'Next'}
          </button>
        {/if}
      </div>
    </div>
  </div>
{/if}
