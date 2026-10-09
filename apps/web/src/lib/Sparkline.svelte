<script lang="ts">
  // Small area chart for a rolling series. `max` fixes the scale (e.g. 100
  // for percentages); otherwise it follows the series peak.
  let { values, max, color = 'var(--color-primary)', label }: { values: number[]; max?: number; color?: string; label: string } = $props();
  const W = 240, H = 56;
  const top = $derived(Math.max(max ?? 0, ...values, 1e-9));
  const points = $derived(values.map((v, i) => `${(i / Math.max(1, values.length - 1)) * W},${H - (v / top) * (H - 4) - 2}`));
</script>

<svg viewBox={`0 0 ${W} ${H}`} class="block h-14 w-full" preserveAspectRatio="none" role="img" aria-label={label}>
  <line x1="0" x2={W} y1={H - 2} y2={H - 2} stroke="var(--color-border)" stroke-width="1"/>
  {#if values.length > 1}
    <polygon points={`0,${H} ${points.join(' ')} ${W},${H}`} fill={color} opacity="0.15"/>
    <polyline points={points.join(' ')} fill="none" stroke={color} stroke-width="2" vector-effect="non-scaling-stroke" stroke-linejoin="round"/>
  {/if}
</svg>
