/**
 * While a floating card is open, close it on the first scroll that happens
 * outside it — anchored content otherwise rides its trigger out of the
 * viewport. Scroll events don't bubble, but they do reach window in the
 * capture phase, so scrolls inside overflow containers (panels, tables) are
 * caught too. Scrolls inside the floating content itself (matched via
 * `contentSelector`) are ignored so the card stays usable.
 *
 * Returns the remove function — designed for `$effect`:
 *
 *   $effect(() => {
 *     if (!open) return;
 *     return closeOnScrollOutside('[data-link-preview-content]', () => (open = false));
 *   });
 */
export function closeOnScrollOutside(contentSelector: string, close: () => void): () => void {
  const onScroll = (e: Event) => {
    if (e.target instanceof Element && e.target.closest(contentSelector)) return;
    close();
  };
  window.addEventListener('scroll', onScroll, { capture: true, passive: true });
  return () => window.removeEventListener('scroll', onScroll, { capture: true });
}
