<script lang="ts">
  let {
    value,
    class: className = '',
  }: {
    value: string | null | undefined;
    class?: string;
  } = $props();

  function relative(iso: string): string {
    const diff = Date.now() - new Date(iso).getTime();
    const s = Math.floor(diff / 1000);
    if (s < 60) return 'just now';
    const m = Math.floor(s / 60);
    if (m < 60) return `${m}m ago`;
    const h = Math.floor(m / 60);
    if (h < 24) return `${h}h ago`;
    const d = Math.floor(h / 24);
    if (d < 30) return `${d}d ago`;
    return new Date(iso).toLocaleDateString();
  }

  function absolute(iso: string): string {
    return new Date(iso).toLocaleString();
  }
</script>

{#if value}
  <time datetime={value} title={absolute(value)} class={className}>{relative(value)}</time>
{:else}
  <span class={className}>—</span>
{/if}
