#!/usr/bin/env bash
# Rebuilds apps/web/src/lib/assets/terrain-atlas.png from Kenney's CC0
# "Topdown Shooter" pack. Cell order must match ATLAS in WorldView's
# terrain.ts. Requires curl, unzip, and ImageMagick 7 (`magick`).
set -euo pipefail
url="https://kenney.nl/media/pages/assets/top-down-shooter/230204340a-1677694684/kenney_top-down-shooter.zip"
out="$(cd "$(dirname "$0")/.." && pwd)/apps/web/src/lib/assets/terrain-atlas.png"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
curl -fsSL "$url" -o "$work/pack.zip"
unzip -q "$work/pack.zip" -d "$work/pack"
sheet="$work/pack/Tilesheet/tilesheet_complete.png"
# name:col,row[:size] on the 64 px source grid; size 2 crops a 128 px sprite.
cells=(
  grass_a:0,0 grass_b:1,0 grass_c:16,0 dirt_a:4,0 dirt_b:5,0 stone_a:6,0 stone_b:8,0 stone_c:10,0
  sand_a:12,0 sand_b:14,0 asphalt:12,3 water:18,0 plaza:11,0 brick:15,2 plank:14,1 tanbrick:16,3
  crate:20,4 crate_small:21,4 rock:20,8 rock_b:21,8 barrel:18,11 barrel_grey:19,11 bush:20,6 tree:18,6:2
  tree_autumn:21,6:2
)
i=0
for cell in "${cells[@]}"; do
  IFS=: read -r _ pos size <<<"$cell"
  col=${pos%,*}; row=${pos#*,}; span=$((64 * ${size:-1}))
  magick "$sheet" -crop "${span}x${span}+$((col * 64))+$((row * 64))" +repage -resize 64x64 "$work/$(printf %03d $i).png"
  if [[ $cell == asphalt:* ]]; then
    # The source cell carries a road-marking dot; paint it out with the base tone.
    base=$(magick "$work/$(printf %03d $i).png" -format '%[pixel:p{4,4}]' info:)
    magick "$work/$(printf %03d $i).png" -fill "$base" -draw 'circle 32,32 32,39' "$work/$(printf %03d $i).png"
  fi
  i=$((i + 1))
done
magick montage "$work"/[0-9]*.png -tile 8x -geometry 64x64+0+0 -background none "$out"
echo "wrote $out"
