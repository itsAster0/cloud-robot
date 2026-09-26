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
  class={cn('h-1.5 w-full cursor-pointer appearance-none rounded-full bg-[#1e3527] accent-[#dfff86] disabled:cursor-not-allowed disabled:opacity-50', klass)}
/>
