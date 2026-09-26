<script lang="ts">
  import { tv, type VariantProps } from 'tailwind-variants';
  import { cn } from './utils';

  const button = tv({
    base: 'inline-flex shrink-0 cursor-pointer items-center justify-center gap-2 rounded-md text-sm font-medium whitespace-nowrap transition-colors outline-none focus-visible:ring-2 focus-visible:ring-[#dfff86] disabled:pointer-events-none disabled:opacity-45 aria-invalid:border-red-500',
    variants: {
      variant: {
        default: 'bg-[#dfff86] text-[#142417] hover:bg-[#e6ff9e]',
        secondary: 'border border-[#45664c] bg-[#192e22] text-[#dfe9df] hover:border-[#dfff86] hover:text-[#dfff86]',
        ghost: 'text-[#a3b9a9] hover:bg-[#192e22] hover:text-[#dfe9df]',
        destructive: 'border border-red-500/50 bg-red-500/10 text-red-400 hover:bg-red-500/20',
      },
      size: {
        default: 'h-9 px-4 py-2',
        sm: 'h-8 px-3 text-xs',
        lg: 'h-10 px-6',
        icon: 'h-9 w-9',
      },
    },
    defaultVariants: { variant: 'default', size: 'default' },
  });

  type Props = VariantProps<typeof button> & {
    class?: string;
    disabled?: boolean;
    type?: 'button' | 'submit';
    onclick?: (e: MouseEvent) => void;
    children?: import('svelte').Snippet;
  };
  let { class: klass = '', variant, size, disabled, type = 'button', onclick, children }: Props = $props();
</script>

<button {type} {disabled} {onclick} class={cn(button({ variant, size }), klass)}>
  {@render children?.()}
</button>
