<script lang="ts">
  import { cn } from './utils';
  let { class: klass = '', children }: { class?: string; children?: import('svelte').Snippet } = $props();
</script>

<div class={cn('rounded-lg border border-[#304d3b] bg-[#0c1c15] text-[#dbe7dc] shadow-sm', klass)}>
  {@render children?.()}
</div>
