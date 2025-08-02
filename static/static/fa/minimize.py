#!/usr/bin/env python3
"""
FontAwesome Font Minimization Tool

Creates minimal font and CSS files containing only the icons specified in a spec file.
"""

import argparse
import re
import sys
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, List, Optional, Set, Tuple

from fontTools.ttLib import TTFont
from fontTools.subset import Subsetter


# CSS Constants
BASE_STYLES = """.fa-solid,
.fa-regular,
.fa-brands,
.fa-classic,
.fas,
.far,
.fab,
.fa {
  --_fa-family: var(--fa-family, var(--fa-style-family, inherit));
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
  display: var(--fa-display, inline-block);
  font-family: var(--_fa-family);
  font-feature-settings: normal;
  font-style: normal;
  font-synthesis: none;
  font-variant: normal;
  font-weight: var(--fa-style, 900);
  line-height: 1;
  text-align: center;
  text-rendering: auto;
  width: var(--fa-width, 1.25em);
}

:is(.fas,
.far,
.fab,
.fa-solid,
.fa-regular,
.fa-brands,
.fa-classic,
.fa)::before {
  content: var(--fa);
  content: var(--fa)/"";
}"""

BASE_BRANDS_STYLES = """/* Brand icon family */
.fab, .fa-brands {
  --fa-family: var(--fa-family-combined);
}"""

BASE_REGULAR_STYLES = """/* Regular icon family */
.far, .fa-regular {
  --fa-family: var(--fa-family-combined);
  --fa-style: 400;
}"""

BASE_SOLID_STYLES = """/* Solid icon family */
.fas, .fa-solid {
  --fa-family: var(--fa-family-combined);
  --fa-style: 900;
}"""


@dataclass
class IconInfo:
    """Information about a single icon including CSS and unicode codepoint."""
    family: str
    icon: str
    css_rule: str
    unicode_codepoint: str
    
    @property
    def css_class_name(self) -> str:
        """Return the full CSS class name (e.g., 'fa-github')."""
        return f"fa-{self.icon}"
    
    @property
    def family_class_name(self) -> str:
        """Return the family CSS class name (e.g., 'fa-brands')."""
        return f"fa-{self.family}"


def parse_spec_file(spec_path: str) -> List[Tuple[str, str]]:
    """
    Parse a spec file and return a list of (family, icon) pairs.
    
    Args:
        spec_path: Path to the spec file containing CSS class pairs
        
    Returns:
        List of (family, icon) tuples, e.g. [("brands", "github"), ("solid", "house")]
        
    Raises:
        FileNotFoundError: If spec file doesn't exist
        ValueError: If line format is invalid
    """
    spec_file = Path(spec_path)
    if not spec_file.exists():
        raise FileNotFoundError(f"Spec file not found: {spec_path}")
    
    pairs = []
    with open(spec_file, 'r') as f:
        for line_num, line in enumerate(f, 1):
            line = line.strip()
            if not line or line.startswith('#'):
                continue
                
            parts = line.split()
            if len(parts) != 2:
                raise ValueError(f"Invalid format at line {line_num}: '{line}'. Expected 'fa-family fa-icon'")
            
            family_class, icon_class = parts
            
            # Extract family name (brands, solid, regular)
            if family_class.startswith('fa-'):
                family = family_class[3:]  # Remove 'fa-' prefix
            elif family_class in ['fas', 'far', 'fab']:
                # Legacy class names
                family_map = {'fas': 'solid', 'far': 'regular', 'fab': 'brands'}
                family = family_map[family_class]
            else:
                raise ValueError(f"Invalid family class at line {line_num}: '{family_class}'")
            
            # Extract icon name
            if not icon_class.startswith('fa-'):
                raise ValueError(f"Invalid icon class at line {line_num}: '{icon_class}'. Must start with 'fa-'")
            
            icon = icon_class[3:]  # Remove 'fa-' prefix
            pairs.append((family, icon))
    
    return pairs


def extract_icon_css(family: str, icon: str) -> Optional[IconInfo]:
    """
    Extract CSS rule and unicode codepoint for a specific icon.
    
    Args:
        family: Icon family (brands, solid, regular)
        icon: Icon name (without fa- prefix)
        
    Returns:
        IconInfo object with CSS and unicode data, or None if not found
    """
    # Map family to likely CSS files
    family_css_files = {
        'brands': ['brands.css'],
        'solid': ['fontawesome.css', 'solid.css'],
        'regular': ['fontawesome.css', 'regular.css']
    }
    
    css_files = family_css_files.get(family, ['fontawesome.css'])
    
    for css_file in css_files:
        css_path = Path(css_file)
        if not css_path.exists():
            continue
            
        try:
            with open(css_path, 'r') as f:
                content = f.read()
                
            # Look for the icon class definition
            icon_class = f"fa-{icon}"
            pattern = rf'\.{re.escape(icon_class)}\s*\{{\s*--fa:\s*"([^"]+)";?\s*\}}'
            match = re.search(pattern, content)
            
            if match:
                unicode_codepoint = match.group(1)
                css_rule = f'.{icon_class} {{\n  --fa: "{unicode_codepoint}";\n}}'
                
                return IconInfo(
                    family=family,
                    icon=icon,
                    css_rule=css_rule,
                    unicode_codepoint=unicode_codepoint
                )
        except IOError:
            continue
    
    return None


def extract_all_icons_css(spec_pairs: List[Tuple[str, str]]) -> List[IconInfo]:
    """
    Extract CSS information for all icons in the spec.
    
    Args:
        spec_pairs: List of (family, icon) tuples from parse_spec_file
        
    Returns:
        List of IconInfo objects with CSS and unicode data
        
    Raises:
        ValueError: If any icon is not found in CSS files
    """
    icon_infos = []
    missing_icons = []
    
    for family, icon in spec_pairs:
        icon_info = extract_icon_css(family, icon)
        if icon_info:
            icon_infos.append(icon_info)
        else:
            missing_icons.append(f"{family}:{icon}")
    
    if missing_icons:
        raise ValueError(f"Icons not found in CSS files: {', '.join(missing_icons)}")
    
    return icon_infos


def generate_minimal_css(icon_infos: List[IconInfo], font_url: str = None, output_prefix: str = "icons") -> str:
    """
    Generate minimal CSS file content from icon information.
    
    Args:
        icon_infos: List of IconInfo objects
        font_url: Custom URL for the font file (optional)
        output_prefix: Output file prefix for default font URL
        
    Returns:
        Complete CSS file content as string
    """
    if not icon_infos:
        return ""
    
    # Group icons by family
    families = {}
    for info in icon_infos:
        if info.family not in families:
            families[info.family] = []
        families[info.family].append(info)
    
    css_parts = []
    
    # Add header comment
    css_parts.append("/*!")
    css_parts.append(" * Minimal FontAwesome CSS - Generated by minimize.py")
    css_parts.append(" * Contains only the icons specified in the spec file")
    css_parts.append(" */")
    css_parts.append("")
    
    # Determine font URL
    if font_url is None:
        font_url = f"{output_prefix}.woff2"
    
    # Add CSS variables at the top
    css_parts.append("/* CSS Variables */")
    css_parts.append(":root, :host {")
    css_parts.append('  --fa-family-combined: "Font Awesome 7 Combined";')
    css_parts.append('  --fa-font: normal 400 1em/1 var(--fa-family-combined);')
    css_parts.append("}")
    css_parts.append("")
    
    # Add base FontAwesome styles
    css_parts.append(BASE_STYLES)
    css_parts.append("")
    
    # Add single font-face declaration for the combined font
    css_parts.append("@font-face {")
    css_parts.append('  font-family: "Font Awesome 7 Combined";')
    css_parts.append("  font-style: normal;")
    css_parts.append("  font-weight: 400 900;")
    css_parts.append("  font-display: block;")
    css_parts.append(f'  src: url("{font_url}");')
    css_parts.append("}")
    css_parts.append("")
    
    # Add family-specific class definitions
    if 'brands' in families:
        css_parts.append(BASE_BRANDS_STYLES)
        css_parts.append("")
    
    if 'regular' in families:
        css_parts.append(BASE_REGULAR_STYLES)
        css_parts.append("")
    
    if 'solid' in families:
        css_parts.append(BASE_SOLID_STYLES)
        css_parts.append("")

    # Add individual icon rules
    css_parts.append("/* Individual icon definitions */")
    # Use specific order: brands, regular, solid
    family_order = ['brands', 'regular', 'solid']
    for family_name in family_order:
        if family_name not in families:
            continue
        family_icons = families[family_name]
        css_parts.append(f"/* {family_name.title()} icons */")
        
        for info in sorted(family_icons, key=lambda x: x.icon):
            css_parts.append(info.css_rule)
        css_parts.append("")
    
    return "\n".join(css_parts)


def unicode_string_to_codepoint(unicode_str: str) -> int:
    """
    Convert a unicode escape string like '\\f09b' to a codepoint integer.
    
    Args:
        unicode_str: Unicode escape string (e.g., '\\f09b')
        
    Returns:
        Integer codepoint (e.g., 61595 for '\\f09b')
    """
    # Remove the backslashes and convert from hex
    hex_part = unicode_str.replace('\\', '')
    return int(hex_part, 16)


def extract_codepoints_by_family(icon_infos: List[IconInfo]) -> Dict[str, Set[int]]:
    """
    Extract codepoints grouped by font family from icon information.
    
    Args:
        icon_infos: List of IconInfo objects
        
    Returns:
        Dictionary mapping family names to sets of codepoints
    """
    family_codepoints = {}
    
    for info in icon_infos:
        if info.family not in family_codepoints:
            family_codepoints[info.family] = set()
        
        codepoint = unicode_string_to_codepoint(info.unicode_codepoint)
        family_codepoints[info.family].add(codepoint)
    
    return family_codepoints


def subset_font_file(source_font_path: str, codepoints: Set[int], output_path: str) -> None:
    """
    Create a subset of a font file containing only specified codepoints.
    
    Args:
        source_font_path: Path to the source WOFF2 file
        codepoints: Set of Unicode codepoints to include
        output_path: Path for the output subset font file
        
    Raises:
        FileNotFoundError: If source font file doesn't exist
        Exception: If font processing fails
    """
    source_path = Path(source_font_path)
    if not source_path.exists():
        raise FileNotFoundError(f"Font file not found: {source_font_path}")
    
    # Load the source font
    font = TTFont(source_font_path)
    
    # Create subsetter and configure options
    subsetter = Subsetter()
    
    # Configure subsetting options for optimal output
    subsetter.options.desubroutinize = True  # Remove subroutines for smaller size
    subsetter.options.layout_features = []  # Remove layout features we don't need
    subsetter.options.name_IDs = ['*']  # Keep all name records
    subsetter.options.notdef_outline = True  # Keep .notdef glyph
    
    # Subset the font to include only the specified codepoints
    subsetter.populate(unicodes=codepoints)
    subsetter.subset(font)
    
    # Save the subset font
    font.save(output_path)
    font.close()


def combine_font_subsets(family_codepoints: Dict[str, Set[int]], output_path: str) -> None:
    """
    Create a combined font file from multiple family subsets.
    
    This uses a simpler approach: find the font family with the most icons
    and create a subset from that source font containing all required codepoints.
    
    Args:
        family_codepoints: Dictionary mapping family names to sets of codepoints
        output_path: Path for the output combined font file
        
    Raises:
        FileNotFoundError: If any source font files don't exist
        ValueError: If no families are provided
    """
    if not family_codepoints:
        raise ValueError("No font families provided")
    
    # Map family names to their source font files
    family_font_files = {
        'brands': 'fa-brands-400.woff2',
        'regular': 'fa-regular-400.woff2',
        'solid': 'fa-solid-900.woff2'
    }
    
    # Combine all codepoints from all families
    all_codepoints = set()
    for codepoints in family_codepoints.values():
        all_codepoints.update(codepoints)
    
    # Find which source font contains the most of our required codepoints
    best_family = None
    best_coverage = 0
    
    for family in family_codepoints.keys():
        if family not in family_font_files:
            continue
        
        source_font_path = family_font_files[family]
        if not Path(source_font_path).exists():
            continue
        
        # Check coverage by loading the font and checking its cmap
        try:
            font = TTFont(source_font_path)
            cmap = font['cmap'].getBestCmap()
            
            # Count how many of our required codepoints this font contains
            coverage = len(all_codepoints.intersection(set(cmap.keys())))
            
            if coverage > best_coverage:
                best_coverage = coverage
                best_family = family
            
            font.close()
        except Exception:
            continue
    
    if not best_family:
        raise ValueError("No suitable source font found")
    
    # If we can't find all codepoints in a single font, we'll create multiple subsets
    # and merge them using the fontTools.merge module
    if best_coverage < len(all_codepoints):
        # Create separate subsets for each family and merge them
        temp_files = []
        
        try:
            for family, codepoints in family_codepoints.items():
                if family not in family_font_files:
                    continue
                
                source_font_path = family_font_files[family]
                temp_subset_path = f"temp_{family}_subset.woff2"
                
                # Create subset of this family
                subset_font_file(source_font_path, codepoints, temp_subset_path)
                temp_files.append(temp_subset_path)
            
            if len(temp_files) == 1:
                # Only one family - just rename the file
                Path(temp_files[0]).rename(output_path)
            else:
                # Multiple families - use fontTools.merge (simpler than manual merging)
                from fontTools.merge import Merger
                
                merger = Merger()
                merged_font = merger.merge(temp_files)
                merged_font.save(output_path)
                merged_font.close()
            
        finally:
            # Clean up temporary files
            for temp_file in temp_files:
                temp_path = Path(temp_file)
                if temp_path.exists():
                    temp_path.unlink()
    
    else:
        # All codepoints can be found in the best font - create a single subset
        source_font_path = family_font_files[best_family]
        subset_font_file(source_font_path, all_codepoints, output_path)
    
    # Report what was combined
    total_icons = len(all_codepoints)
    families_list = list(family_codepoints.keys())
    print(f"Combined {total_icons} icons from {len(families_list)} families: {', '.join(families_list)}")


def generate_minimal_font(icon_infos: List[IconInfo], output_path: str) -> None:
    """
    Generate a minimal font file containing only the specified icons.
    
    Args:
        icon_infos: List of IconInfo objects with font and codepoint data
        output_path: Path for the output font file
        
    Raises:
        ValueError: If no icons are provided
        FileNotFoundError: If source font files are missing
    """
    if not icon_infos:
        raise ValueError("No icons provided for font generation")
    
    # Extract codepoints by family
    family_codepoints = extract_codepoints_by_family(icon_infos)
    
    # If we only have one family, create a simple subset
    if len(family_codepoints) == 1:
        family = next(iter(family_codepoints.keys()))
        codepoints = family_codepoints[family]
        
        family_font_files = {
            'brands': 'fa-brands-400.woff2',
            'regular': 'fa-regular-400.woff2',
            'solid': 'fa-solid-900.woff2'
        }
        
        source_font = family_font_files.get(family)
        if not source_font:
            raise ValueError(f"Unknown font family: {family}")
        
        subset_font_file(source_font, codepoints, output_path)
    else:
        # Multiple families - create combined font
        combine_font_subsets(family_codepoints, output_path)


def main():
    """Main CLI entry point."""
    parser = argparse.ArgumentParser(
        description="Create minimal FontAwesome font and CSS files from a spec file",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  %(prog)s icons.spec
  %(prog)s icons.spec --output-prefix custom
  %(prog)s icons.spec --url "/fonts/custom-icons.woff2"
  %(prog)s icons.spec --css-only
        """
    )
    
    parser.add_argument(
        'spec_file',
        help='Spec file containing CSS class pairs (one per line)'
    )
    
    parser.add_argument(
        '--output-prefix',
        default=None,
        help='Output file prefix (default: same as spec file without extension)'
    )
    
    parser.add_argument(
        '--css-only',
        action='store_true',
        help='Generate only CSS file, not font file'
    )
    
    parser.add_argument(
        '--font-only',
        action='store_true',
        help='Generate only font file, not CSS file'
    )
    
    parser.add_argument(
        '--url',
        default=None,
        help='Custom URL for the font file in CSS (default: same as output prefix + .woff2)'
    )
    
    parser.add_argument(
        '--verbose', '-v',
        action='store_true',
        help='Enable verbose output'
    )
    
    args = parser.parse_args()
    
    if args.css_only and args.font_only:
        parser.error("Cannot specify both --css-only and --font-only")
    
    try:
        # Parse the spec file
        pairs = parse_spec_file(args.spec_file)
        
        if args.verbose:
            print(f"Parsed {len(pairs)} icon specifications:")
            for family, icon in pairs:
                print(f"  {family}: {icon}")
        
        # Extract CSS information for all icons
        if args.verbose:
            print("Extracting CSS information...")
        
        icon_infos = extract_all_icons_css(pairs)
        
        if args.verbose:
            print(f"Successfully extracted CSS for {len(icon_infos)} icons:")
            for info in icon_infos:
                print(f"  {info.family}:{info.icon} -> {info.unicode_codepoint}")
        
        # Determine output prefix
        if args.output_prefix:
            output_prefix = args.output_prefix
        else:
            output_prefix = Path(args.spec_file).stem
        
        # Generate CSS file if requested
        if not args.font_only:
            css_content = generate_minimal_css(icon_infos, args.url, output_prefix)
            css_filename = f"{output_prefix}.css"
            
            with open(css_filename, 'w') as f:
                f.write(css_content)
            
            print(f"Generated: {css_filename}")
        
        # Generate font file if requested
        if not args.css_only:
            if args.verbose:
                print("Generating minimal font file...")
                family_codepoints = extract_codepoints_by_family(icon_infos)
                for family, codepoints in family_codepoints.items():
                    print(f"  {family}: {len(codepoints)} icons")
            
            font_filename = f"{output_prefix}.woff2"
            generate_minimal_font(icon_infos, font_filename)
            
            print(f"Generated: {font_filename}")
        
    except (FileNotFoundError, ValueError) as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == '__main__':
    main()