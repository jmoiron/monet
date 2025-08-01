#!/usr/bin/env python3
"""
FontAwesome Font Minimization Tool

Creates minimal font and CSS files containing only the icons specified in a spec file.
"""

import argparse
import sys
from pathlib import Path
from typing import List, Tuple


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
        
        # Determine output prefix
        if args.output_prefix:
            output_prefix = args.output_prefix
        else:
            output_prefix = Path(args.spec_file).stem
        
        print(f"Would generate: {output_prefix}.woff2 and {output_prefix}.css")
        print("(Font generation not yet implemented)")
        
    except (FileNotFoundError, ValueError) as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == '__main__':
    main()