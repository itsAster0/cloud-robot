<script lang="ts">
  import { cn } from './utils';
  // value and max in the same units; the bar clamps to 0..100%.
  let { value = 0, max = 100, class: klass = '', barClass = 'bg-primary', label }: { value?: number; max?: number; class?: string; barClass?: string; label?: string } = $props();
  let percent = $derived(max > 0 ? Math.max(0, Math.min(100, (value / max) * 100)) : 0);
</script>

<div class={cn('h-2 w-full overflow-hidden rounded-full bg-muted', klass)} role="progressbar" aria-label={label} aria-valuemin={0} aria-valuemax={max} aria-valuenow={Math.round(value)}>
  <div class={cn('h-full rounded-full transition-[width] duration-300', barClass)} style:width={`${percent}%`}></div>
</div>
