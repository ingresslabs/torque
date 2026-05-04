#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT_DIR="$ROOT_DIR/docs/assets/logo"
mkdir -p "$OUT_DIR"

ICON="$OUT_DIR/torque-logo-icon.png"
ICON_SMALL="$OUT_DIR/torque-logo-icon-256.png"
MARK_PNG="$OUT_DIR/.torque-logo-mark-render.png"

BLUE="#326CE5"
BLUE_SOFT="#6B9EFF"
SURFACE="#ffffff"
SURFACE_SOFT="#fcfdff"
BORDER="#d9e6ff"

# Draw mark directly to avoid SVG raster inconsistencies.
magick -size 920x920 xc:none \
  -stroke "$BLUE" -strokewidth 20 -fill none -draw "circle 460,460 460,76" \
  -stroke "rgba(107,158,255,0.55)" -strokewidth 3 -draw "circle 460,460 460,104" \
  -stroke "$BLUE" -strokewidth 22 -draw "line 240,268 680,268" -draw "line 460,268 460,700" \
  -strokewidth 16 -draw "line 350,700 570,700" \
  -strokewidth 10 -stroke "rgba(50,108,229,0.78)" \
    -draw "path 'M 240,560 C 314,693 483,754 621,681'" \
    -draw "path 'M 680,360 C 606,227 437,166 299,239'" \
  -stroke none -fill "$BLUE_SOFT" -draw "circle 460,460 460,434" \
  -fill "$SURFACE" -draw "circle 460,460 460,451" \
  "$MARK_PNG"

# App icon
magick -size 1024x1024 xc:none \
  -fill "$SURFACE_SOFT" -stroke none -draw "roundrectangle 64,64 960,960 210,210" \
  -stroke "$BORDER" -strokewidth 5 -fill none -draw "roundrectangle 64,64 960,960 210,210" \
  \( "$MARK_PNG" -resize 704x704 \) -gravity center -compose Over -composite \
  "$ICON"

magick "$ICON" -resize 256x256 "$ICON_SMALL"

rm -f "$MARK_PNG"

printf 'Generated:\n- %s\n- %s\n' "$ICON" "$ICON_SMALL"
