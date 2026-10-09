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
    'm-0 h-9 w-full rounded-md border border-input bg-background px-3 py-1 font-sans text-sm text-foreground shadow-xs outline-none transition-colors placeholder:text-muted-foreground focus:border-ring disabled:cursor-not-allowed disabled:opacity-50',
    klass,
  )}
/>
