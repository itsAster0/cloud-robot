<script lang="ts">
  import { cn } from './utils';
  let { class: klass = '', children, htmlFor }: { class?: string; children?: import('svelte').Snippet; htmlFor?: string } = $props();
</script>

<label for={htmlFor} class={cn('font-sans text-xs font-medium tracking-normal normal-case text-muted-foreground', klass)}>
  {@render children?.()}
</label>
