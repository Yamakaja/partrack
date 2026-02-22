<script lang="ts">
  import { type Snippet } from 'svelte';
  import Sidebar from './Sidebar.svelte';

  let { children }: { children: Snippet } = $props();
</script>

<div class="flex h-screen overflow-hidden bg-background">
  <Sidebar />
  <main class="flex-1 overflow-y-auto">
    {@render children()}
  </main>
</div>
