<script lang="ts">
  import { cn } from './utils';
  let { class: klass = '', children }: { class?: string; children?: import('svelte').Snippet } = $props();
</script>

<div class={cn('flex flex-col gap-1.5 p-5 pb-3', klass)}>
  {@render children?.()}
</div>
