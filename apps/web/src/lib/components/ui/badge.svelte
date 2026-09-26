<script lang="ts">
  import { tv, type VariantProps } from 'tailwind-variants';
  import { cn } from './utils';

  const badge = tv({
    base: 'inline-flex items-center gap-1 rounded-md border px-2 py-0.5 font-mono text-[11px] font-medium whitespace-nowrap',
    variants: {
      variant: {
        default: 'border-[#304d3b] bg-[#13271a] text-[#dbe7dc]',
        live: 'border-[#dfff86]/50 bg-[#dfff86]/10 text-[#dfff86]',
        muted: 'border-[#304d3b] text-[#a3b9a9]',
        warn: 'border-red-500/50 bg-red-500/10 text-red-300',
      },
    },
    defaultVariants: { variant: 'default' },
  });

  let { class: klass = '', variant, children }: VariantProps<typeof badge> & { class?: string; children?: import('svelte').Snippet } = $props();
</script>

<span class={cn(badge({ variant }), klass)}>
  {@render children?.()}
</span>
