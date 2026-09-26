<script lang="ts" generics="T extends string | number">
  import { cn } from './utils';
  // Accessible single-choice group rendered as cards (layout="cards") or a
  // compact segmented control (layout="segmented"). Arrow keys move selection.
  interface Choice { value: T; label: string; description?: string; meta?: string; icon?: string; disabled?: boolean }
  let {
    value = $bindable(),
    choices,
    ariaLabel,
    layout = 'cards',
    class: klass = '',
    onchange,
  }: { value: T; choices: Choice[]; ariaLabel: string; layout?: 'cards' | 'segmented'; class?: string; onchange?: (value: T) => void } = $props();

  function pick(next: T) {
    if (next === value) return;
    value = next;
    onchange?.(next);
  }
  function keydown(event: KeyboardEvent, index: number) {
    const step = event.key === 'ArrowRight' || event.key === 'ArrowDown' ? 1 : event.key === 'ArrowLeft' || event.key === 'ArrowUp' ? -1 : 0;
    if (!step) return;
    event.preventDefault();
    const enabled = choices.filter(choice => !choice.disabled);
    const current = enabled.findIndex(choice => choice.value === choices[index].value);
    const next = enabled[(current + step + enabled.length) % enabled.length];
    pick(next.value);
    (event.currentTarget as HTMLElement).parentElement?.querySelector<HTMLElement>(`[data-value="${next.value}"]`)?.focus();
  }
</script>

<div role="radiogroup" aria-label={ariaLabel} class={cn(layout === 'cards' ? 'grid gap-2' : 'inline-flex w-full rounded-lg border border-border bg-background p-1', klass)}>
  {#each choices as choice, index (choice.value)}
    {@const active = choice.value === value}
    <button
      type="button"
      role="radio"
      aria-checked={active}
      tabindex={active ? 0 : -1}
      data-value={choice.value}
      disabled={choice.disabled}
      onclick={() => pick(choice.value)}
      onkeydown={event => keydown(event, index)}
      class={cn(
        'cursor-pointer text-left transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:cursor-not-allowed disabled:opacity-40',
        layout === 'cards'
          ? 'flex items-start gap-3 rounded-lg border p-3 ' + (active ? 'border-primary bg-accent' : 'border-border bg-background hover:border-input hover:bg-muted/60')
          : 'flex-1 rounded-md px-2 py-1.5 text-center text-xs font-medium ' + (active ? 'bg-primary text-primary-foreground' : 'text-muted-foreground hover:text-foreground'),
      )}
    >
      {#if layout === 'cards'}
        {#if choice.icon}<span class={cn('grid size-9 shrink-0 place-items-center rounded-md border text-base', active ? 'border-primary/50 text-primary' : 'border-border text-muted-foreground')} aria-hidden="true">{choice.icon}</span>{/if}
        <span class="min-w-0 flex-1">
          <span class="flex items-center justify-between gap-2"><span class={cn('text-sm font-semibold', active ? 'text-primary' : 'text-foreground')}>{choice.label}</span>{#if choice.meta}<span class="font-mono text-[10px] text-muted-foreground">{choice.meta}</span>{/if}</span>
          {#if choice.description}<span class="mt-0.5 block text-xs leading-snug text-muted-foreground">{choice.description}</span>{/if}
        </span>
      {:else}
        {choice.label}
      {/if}
    </button>
  {/each}
</div>
