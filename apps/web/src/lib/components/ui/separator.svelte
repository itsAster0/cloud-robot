<script lang="ts">
  import { cn } from './utils';
  let { class: klass = '' }: { class?: string } = $props();
</script>

<div role="separator" class={cn('h-px w-full shrink-0 bg-border', klass)}></div>
