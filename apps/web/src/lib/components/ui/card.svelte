<script lang="ts">
  import { cn } from './utils';
  let { class: klass = '', children }: { class?: string; children?: import('svelte').Snippet } = $props();
</script>

<div class={cn('rounded-xl border border-border bg-card text-card-foreground shadow-sm', klass)}>
  {@render children?.()}
</div>
