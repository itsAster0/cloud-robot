<script lang="ts">
  import { cn } from './utils';
  let { class: klass = '', children }: { class?: string; children?: import('svelte').Snippet } = $props();
</script>

<p class={cn('text-xs text-muted-foreground', klass)}>
  {@render children?.()}
</p>
