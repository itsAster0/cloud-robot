<script lang="ts">
  import { cn } from './utils';
  let { class: klass = '', children }: { class?: string; children?: import('svelte').Snippet } = $props();
</script>

<h3 class={cn('text-base leading-tight font-semibold tracking-tight', klass)}>
  {@render children?.()}
</h3>
