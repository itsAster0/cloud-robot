<script lang="ts">
  import { tv, type VariantProps } from 'tailwind-variants';
  import { cn } from './utils';

  const badge = tv({
    base: 'inline-flex items-center gap-1 rounded-md border px-2 py-0.5 font-mono text-[11px] font-medium whitespace-nowrap',
    variants: {
      variant: {
        default: 'border-border bg-secondary text-foreground',
        live: 'border-primary/50 bg-primary/10 text-primary',
        muted: 'border-border text-muted-foreground',
        warn: 'border-warning/50 bg-warning/10 text-warning',
        danger: 'border-destructive/50 bg-destructive/10 text-destructive',
      },
    },
    defaultVariants: { variant: 'default' },
  });

  let { class: klass = '', variant, children }: VariantProps<typeof badge> & { class?: string; children?: import('svelte').Snippet } = $props();
</script>

<span class={cn(badge({ variant }), klass)}>
  {@render children?.()}
</span>
