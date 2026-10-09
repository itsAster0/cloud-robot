<script lang="ts">
  import { cn } from './utils';
  let {
    class: klass = '',
    value = $bindable(0),
    min = 0,
    max = 100,
    step = 1,
    disabled = false,
    ariaLabel,
  }: {
    class?: string;
    value?: number;
    min?: number;
    max?: number;
    step?: number;
    disabled?: boolean;
    ariaLabel?: string;
  } = $props();
</script>

<input
  type="range"
  bind:value
  {min}
  {max}
  {step}
  {disabled}
  aria-label={ariaLabel}
  class={cn('m-0 h-1.5 w-full cursor-pointer appearance-none rounded-full bg-muted accent-primary disabled:cursor-not-allowed disabled:opacity-50', klass)}
/>
