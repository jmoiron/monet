# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a FontAwesome 7.0.0 font minimization tool project. The directory contains:

- **Font files (*.woff2)**: 4 font files containing icons separated into 3 families:
  - `fa-solid-900.woff2` - Solid icons (weight 900)
  - `fa-regular-400.woff2` - Regular icons (weight 400) 
  - `fa-brands-400.woff2` - Brand icons (weight 400)
  - `fa-v4compatibility.woff2` - Legacy v4 compatibility icons

- **CSS files**: 4 stylesheets defining font faces and icon classes:
  - `fontawesome.css` - Base FontAwesome styles and utility classes
  - `solid.css` - Solid font family definitions and @font-face rules
  - `regular.css` - Regular font family definitions
  - `brands.css` - Brands font family definitions

## CSS Class Structure

FontAwesome uses a two-part class system:
1. **Family selectors**: `fa-solid`, `fa-regular`, `fa-brands` (or legacy `fas`, `far`, `fab`)
2. **Icon selectors**: `fa-{icon-name}` (e.g., `fa-circle-info`, `fa-home`)

Icons are displayed using CSS custom properties (`--fa` variables) that contain Unicode codepoints corresponding to the font glyphs.

## Core Task

The main objective is to create a Python tool called `minimize.py` that:

1. **Input**: Takes a "spec file" containing pairs of CSS classes (one per line, e.g., "fa-solid fa-circle-info")
2. **Output**: Produces minimal font (.woff2) and CSS (.css) files containing only the icons specified in the spec file
3. **Dependencies**: Uses `fontTools` Python library, specifically `fontTools.subset` for font subsetting
4. **Functionality**: 
   - Parse spec file to identify required icons and families
   - Extract Unicode codepoints from existing CSS files
   - Create subset font files using fontTools.subset
   - Generate minimal CSS with only required font-face declarations and icon rules

## Development Commands

Since this is a standalone font processing tool, typical commands would be:

```bash
# Install dependencies
pip install fonttools

# Run the minimization tool
python3 minimize.py icons.spec

# Expected output: icons.woff2 and icons.css
```

## Architecture Notes

- The existing CSS files use CSS custom properties (`--fa-*`) for font family management
- Font loading uses `@font-face` declarations with `font-display: block`
- Icon content is set via CSS custom properties rather than direct `content` rules
- The tool needs to maintain the relationship between CSS class names and Unicode codepoints in the font files