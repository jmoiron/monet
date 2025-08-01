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
from typing import Dict, List, Optional, Tuple


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

BASE_BRANDS_STYLES = """/* Brand icons */
:root, :host {
  --fa-family-brands: "Font Awesome 7 Brands";
  --fa-font-brands: normal 400 1em/1 var(--fa-family-brands);
}

@font-face {
  font-family: "Font Awesome 7 Brands";
  font-style: normal;
  font-weight: 400;
  font-display: block;
  src: url("fa-brands-400.woff2");
}

.fab {
  --fa-family: var(--fa-family-brands);
}

.fa-brands {
  --fa-family: var(--fa-family-brands);
}"""

BASE_REGULAR_STYLES = """/* Regular icons */
@font-face {
  font-family: "Font Awesome 7 Free";
  font-style: normal;
  font-weight: 400;
  font-display: block;
  src: url("fa-regular-400.woff2");
}

.far {
  --fa-family: var(--fa-family-classic);
  --fa-style: 400;
}

.fa-regular {
  --fa-style: 400;
}"""

BASE_SOLID_STYLES = """/* Solid icons */
:root, :host {
  --fa-family-classic: "Font Awesome 7 Free";
  --fa-font-solid: normal 900 1em/1 var(--fa-family-classic);
}

@font-face {
  font-family: "Font Awesome 7 Free";
  font-style: normal;
  font-weight: 900;
  font-display: block;
  src: url("fa-solid-900.woff2");
}

.fas {
  --fa-family: var(--fa-family-classic);
  --fa-style: 900;
}

.fa-solid {
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


def generate_minimal_css(icon_infos: List[IconInfo]) -> str:
    """
    Generate minimal CSS file content from icon information.
    
    Args:
        icon_infos: List of IconInfo objects
        
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
    
    # Add base FontAwesome styles
    css_parts.append(BASE_STYLES)
    css_parts.append("")
    
    # Add family-specific styles and font-face declarations in order: brands, regular, solid
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


def main():
    """Main CLI entry point."""
    parser = argparse.ArgumentParser(
        description="Create minimal FontAwesome font and CSS files from a spec file",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  %(prog)s icons.spec
  %(prog)s icons.spec --output-prefix custom
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
            css_content = generate_minimal_css(icon_infos)
            css_filename = f"{output_prefix}.css"
            
            with open(css_filename, 'w') as f:
                f.write(css_content)
            
            print(f"Generated: {css_filename}")
        
        # Font generation placeholder
        if not args.css_only:
            print(f"Font generation for {output_prefix}.woff2 not yet implemented")
        
    except (FileNotFoundError, ValueError) as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == '__main__':
    main()