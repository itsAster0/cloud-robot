<script lang="ts">
  import { cn } from './utils';
  let {
    class: klass = '',
    value = $bindable(''),
    disabled = false,
    ariaLabel,
    children,
    onchange,
  }: {
    class?: string;
    value?: string | number;
    disabled?: boolean;
    ariaLabel?: string;
    children?: import('svelte').Snippet;
    onchange?: (e: Event) => void;
  } = $props();
</script>

<select
  bind:value
  {disabled}
  aria-label={ariaLabel}
  {onchange}
  class={cn(
    'h-9 w-full rounded-md border border-[#38533e] bg-[#07130d] px-3 text-sm text-[#e2eade] outline-none focus:border-[#dfff86] disabled:cursor-not-allowed disabled:opacity-50',
    klass,
  )}
>
  {@render children?.()}
</select>
