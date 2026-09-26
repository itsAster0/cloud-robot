<script lang="ts">
  import { cn } from './utils';
  let { class: klass = '', children }: { class?: string; children?: import('svelte').Snippet } = $props();
</script>

<div class={cn('p-5 pt-3', klass)}>
  {@render children?.()}
</div>
