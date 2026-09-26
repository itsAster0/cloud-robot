<script lang="ts">
  import { cn } from './utils';
  let { class: klass = '', children, htmlFor }: { class?: string; children?: import('svelte').Snippet; htmlFor?: string } = $props();
</script>

<label for={htmlFor} class={cn('text-xs font-medium text-muted-foreground', klass)}>
  {@render children?.()}
</label>
