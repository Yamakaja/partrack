<script lang="ts">
  import { kindIcon, type ObjectKind } from '$lib/components/notes/objectKinds.js';

  // Editor-side chip (inside the ProseMirror node-view). Shows the stored label
  // with a kind icon; the read-only viewer resolves live data from the manifest.
  let { kind, label }: { kind: ObjectKind; label: string } = $props();
  let Icon = $derived(kindIcon(kind));
</script>

<span
  class="inline-flex items-center gap-1 rounded border border-primary/30 bg-primary/10 px-1.5 py-0.5 align-baseline text-sm font-medium text-primary"
  contenteditable="false"
>
  <Icon class="h-3 w-3 shrink-0" />
  {label}
</span>
