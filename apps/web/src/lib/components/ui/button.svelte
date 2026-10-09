<script lang="ts">
  import { tv, type VariantProps } from 'tailwind-variants';
  import { cn } from './utils';

  const button = tv({
    base: 'inline-flex shrink-0 cursor-pointer items-center justify-center gap-2 rounded-md text-sm font-medium whitespace-nowrap transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-45',
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground hover:bg-primary/90',
        secondary: 'border border-input bg-secondary text-secondary-foreground hover:border-primary/60 hover:text-primary',
        outline: 'border border-border bg-transparent text-foreground hover:bg-muted',
        ghost: 'text-muted-foreground hover:bg-muted hover:text-foreground',
        destructive: 'border border-destructive/50 bg-destructive/10 text-destructive hover:bg-destructive/20',
      },
      size: {
        default: 'h-9 px-4 py-2',
        sm: 'h-8 px-3 text-xs',
        lg: 'h-11 px-6 text-base',
        icon: 'h-9 w-9',
      },
    },
    defaultVariants: { variant: 'default', size: 'default' },
  });

  type Props = VariantProps<typeof button> & {
    class?: string;
    disabled?: boolean;
    type?: 'button' | 'submit';
    title?: string;
    ariaLabel?: string;
    onclick?: (e: MouseEvent) => void;
    children?: import('svelte').Snippet;
  };
  let { class: klass = '', variant, size, disabled = false, type = 'button', title, ariaLabel, onclick, children }: Props = $props();
</script>

<button {type} {disabled} {title} aria-label={ariaLabel} {onclick} class={cn(button({ variant, size }), klass)}>
  {@render children?.()}
</button>
