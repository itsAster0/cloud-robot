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
    'm-0 h-9 w-full rounded-md border border-input bg-background px-3 py-0 font-sans text-sm text-foreground outline-none focus:border-ring disabled:cursor-not-allowed disabled:opacity-50',
    klass,
  )}
>
  {@render children?.()}
</select>
