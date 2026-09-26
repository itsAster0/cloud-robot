<script lang="ts">
  import { cn } from './utils';
  let {
    class: klass = '',
    value = $bindable(''),
    type = 'text',
    placeholder = '',
    disabled = false,
    maxlength,
    min,
    max,
    ariaLabel,
  }: {
    class?: string;
    value?: string | number;
    type?: string;
    placeholder?: string;
    disabled?: boolean;
    maxlength?: number;
    min?: number;
    max?: number;
    ariaLabel?: string;
  } = $props();
</script>

<input
  bind:value
  {type}
  {placeholder}
  {disabled}
  {maxlength}
  {min}
  {max}
  aria-label={ariaLabel}
  class={cn(
    'h-9 w-full rounded-md border border-[#38533e] bg-[#07130d] px-3 py-1 text-sm text-[#e2eade] shadow-xs outline-none transition-colors placeholder:text-[#5d7268] focus:border-[#dfff86] disabled:cursor-not-allowed disabled:opacity-50',
    klass,
  )}
/>
