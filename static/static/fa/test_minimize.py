#!/usr/bin/env python3
"""
Tests for the minimize.py FontAwesome minimization tool.
"""

import pytest
import tempfile
import os
from pathlib import Path
from minimize import (
    parse_spec_file, extract_icon_css, extract_all_icons_css, generate_minimal_css, 
    IconInfo, unicode_string_to_codepoint, extract_codepoints_by_family, 
    subset_font_file, generate_minimal_font
)


class TestParseSpecFile:
    """Test cases for the parse_spec_file function."""
    
    def test_parse_valid_spec_file(self):
        """Test parsing a valid spec file with mixed families."""
        spec_content = """fa-brands fa-github
fa-solid fa-house
fa-regular fa-circle
fa-brands fa-instagram"""
        
        with tempfile.NamedTemporaryFile(mode='w', suffix='.spec', delete=False) as f:
            f.write(spec_content)
            f.flush()
            
            try:
                pairs = parse_spec_file(f.name)
                expected = [
                    ('brands', 'github'),
                    ('solid', 'house'),
                    ('regular', 'circle'),
                    ('brands', 'instagram')
                ]
                assert pairs == expected
            finally:
                os.unlink(f.name)
    
    def test_parse_legacy_class_names(self):
        """Test parsing spec file with legacy class names (fas, far, fab)."""
        spec_content = """fab fa-github
fas fa-house
far fa-circle"""
        
        with tempfile.NamedTemporaryFile(mode='w', suffix='.spec', delete=False) as f:
            f.write(spec_content)
            f.flush()
            
            try:
                pairs = parse_spec_file(f.name)
                expected = [
                    ('brands', 'github'),
                    ('solid', 'house'),
                    ('regular', 'circle')
                ]
                assert pairs == expected
            finally:
                os.unlink(f.name)
    
    def test_parse_empty_lines_and_comments(self):
        """Test that empty lines and comments are ignored."""
        spec_content = """# This is a comment
fa-brands fa-github

# Another comment
fa-solid fa-house

"""
        
        with tempfile.NamedTemporaryFile(mode='w', suffix='.spec', delete=False) as f:
            f.write(spec_content)
            f.flush()
            
            try:
                pairs = parse_spec_file(f.name)
                expected = [
                    ('brands', 'github'),
                    ('solid', 'house')
                ]
                assert pairs == expected
            finally:
                os.unlink(f.name)
    
    def test_parse_nonexistent_file(self):
        """Test that FileNotFoundError is raised for nonexistent files."""
        with pytest.raises(FileNotFoundError, match="Spec file not found"):
            parse_spec_file("nonexistent.spec")
    
    def test_parse_invalid_line_format(self):
        """Test that ValueError is raised for invalid line formats."""
        spec_content = """fa-brands fa-github
invalid-line-with-one-part
fa-solid fa-house"""
        
        with tempfile.NamedTemporaryFile(mode='w', suffix='.spec', delete=False) as f:
            f.write(spec_content)
            f.flush()
            
            try:
                with pytest.raises(ValueError, match="Invalid format at line 2"):
                    parse_spec_file(f.name)
            finally:
                os.unlink(f.name)
    
    def test_parse_invalid_family_class(self):
        """Test that ValueError is raised for invalid family classes."""
        spec_content = """invalid-family fa-github"""
        
        with tempfile.NamedTemporaryFile(mode='w', suffix='.spec', delete=False) as f:
            f.write(spec_content)
            f.flush()
            
            try:
                with pytest.raises(ValueError, match="Invalid family class at line 1"):
                    parse_spec_file(f.name)
            finally:
                os.unlink(f.name)
    
    def test_parse_invalid_icon_class(self):
        """Test that ValueError is raised for invalid icon classes."""
        spec_content = """fa-brands invalid-icon"""
        
        with tempfile.NamedTemporaryFile(mode='w', suffix='.spec', delete=False) as f:
            f.write(spec_content)
            f.flush()
            
            try:
                with pytest.raises(ValueError, match="Invalid icon class at line 1"):
                    parse_spec_file(f.name)
            finally:
                os.unlink(f.name)
    
    def test_parse_hyphenated_icon_names(self):
        """Test parsing icon names with hyphens."""
        spec_content = """fa-brands fa-hacker-news
fa-solid fa-right-from-bracket
fa-solid fa-circle-info"""
        
        with tempfile.NamedTemporaryFile(mode='w', suffix='.spec', delete=False) as f:
            f.write(spec_content)
            f.flush()
            
            try:
                pairs = parse_spec_file(f.name)
                expected = [
                    ('brands', 'hacker-news'),
                    ('solid', 'right-from-bracket'),
                    ('solid', 'circle-info')
                ]
                assert pairs == expected
            finally:
                os.unlink(f.name)
    
    def test_parse_actual_icons_spec(self):
        """Test parsing the actual icons.spec file if it exists."""
        icons_spec = Path("icons.spec")
        if icons_spec.exists():
            pairs = parse_spec_file("icons.spec")
            
            # Should contain the icons we created earlier
            expected_icons = [
                ('brands', 'hacker-news'),
                ('brands', 'github'),
                ('brands', 'bluesky'),
                ('brands', 'linkedin'),
                ('brands', 'instagram'),
                ('solid', 'circle-info'),
                ('solid', 'right-from-bracket'),
                ('solid', 'users'),
                ('solid', 'house')
            ]
            
            assert pairs == expected_icons


class TestIconInfo:
    """Test cases for the IconInfo dataclass."""
    
    def test_icon_info_properties(self):
        """Test IconInfo property methods."""
        info = IconInfo(
            family="brands",
            icon="github",
            css_rule='.fa-github {\n  --fa: "\\f09b";\n}',
            unicode_codepoint="\\f09b"
        )
        
        assert info.css_class_name == "fa-github"
        assert info.family_class_name == "fa-brands"


class TestExtractIconCSS:
    """Test cases for the extract_icon_css function."""
    
    def test_extract_existing_brand_icon(self):
        """Test extracting CSS for an existing brand icon."""
        info = extract_icon_css("brands", "github")
        
        assert info is not None
        assert info.family == "brands"
        assert info.icon == "github"
        assert info.unicode_codepoint == "\\f09b"
        assert ".fa-github" in info.css_rule
        assert "\\f09b" in info.css_rule
    
    def test_extract_existing_solid_icon(self):
        """Test extracting CSS for an existing solid icon."""
        info = extract_icon_css("solid", "house")
        
        assert info is not None
        assert info.family == "solid"
        assert info.icon == "house"
        assert info.unicode_codepoint == "\\f015"
        assert ".fa-house" in info.css_rule
        assert "\\f015" in info.css_rule
    
    def test_extract_nonexistent_icon(self):
        """Test that None is returned for nonexistent icons."""
        info = extract_icon_css("brands", "nonexistent-icon")
        assert info is None
    
    def test_extract_hyphenated_icon_names(self):
        """Test extracting icons with hyphenated names."""
        info = extract_icon_css("brands", "hacker-news")
        
        assert info is not None
        assert info.family == "brands"
        assert info.icon == "hacker-news"
        assert info.unicode_codepoint == "\\f1d4"
        assert ".fa-hacker-news" in info.css_rule


class TestExtractAllIconsCSS:
    """Test cases for the extract_all_icons_css function."""
    
    def test_extract_valid_icons(self):
        """Test extracting CSS for valid icon pairs."""
        pairs = [
            ("brands", "github"),
            ("solid", "house"),
            ("brands", "instagram")
        ]
        
        infos = extract_all_icons_css(pairs)
        
        assert len(infos) == 3
        assert infos[0].icon == "github"
        assert infos[1].icon == "house"
        assert infos[2].icon == "instagram"
        
        # Check that unicode codepoints are extracted
        github_info = next(info for info in infos if info.icon == "github")
        assert github_info.unicode_codepoint == "\\f09b"
    
    def test_extract_with_missing_icons(self):
        """Test that ValueError is raised for missing icons."""
        pairs = [
            ("brands", "github"),
            ("brands", "nonexistent-icon"),
            ("solid", "house")
        ]
        
        with pytest.raises(ValueError, match="Icons not found in CSS files"):
            extract_all_icons_css(pairs)
    
    def test_extract_icons_from_spec(self):
        """Test extracting all icons from the actual spec file."""
        if Path("icons.spec").exists():
            pairs = parse_spec_file("icons.spec")
            infos = extract_all_icons_css(pairs)
            
            assert len(infos) == 9
            
            # Check that we have both brands and solid icons
            brands_icons = [info for info in infos if info.family == "brands"]
            solid_icons = [info for info in infos if info.family == "solid"]
            
            assert len(brands_icons) == 5
            assert len(solid_icons) == 4


class TestGenerateMinimalCSS:
    """Test cases for the generate_minimal_css function."""
    
    def test_generate_empty_css(self):
        """Test generating CSS with no icons."""
        css = generate_minimal_css([])
        assert css == ""
    
    def test_generate_css_with_brands_only(self):
        """Test generating CSS with only brand icons."""
        infos = [
            IconInfo("brands", "github", '.fa-github {\n  --fa: "\\f09b";\n}', "\\f09b"),
            IconInfo("brands", "instagram", '.fa-instagram {\n  --fa: "\\f16d";\n}', "\\f16d")
        ]
        
        css = generate_minimal_css(infos)
        
        # Check header
        assert "Minimal FontAwesome CSS" in css
        
        # Check base styles
        assert ".fa-brands" in css
        assert "::before" in css
        
        # Check brand-specific font face
        assert "@font-face" in css
        assert "Font Awesome 7 Brands" in css
        assert 'src: url("fa-brands-400.woff2");' in css
        
        # Check individual icon rules
        assert ".fa-github" in css
        assert ".fa-instagram" in css
        assert "\\f09b" in css
        assert "\\f16d" in css
        
        # Should not contain solid styles
        assert "Font Awesome 7 Free" not in css
    
    def test_generate_css_with_solid_only(self):
        """Test generating CSS with only solid icons."""
        infos = [
            IconInfo("solid", "house", '.fa-house {\n  --fa: "\\f015";\n}', "\\f015"),
            IconInfo("solid", "users", '.fa-users {\n  --fa: "\\f0c0";\n}', "\\f0c0")
        ]
        
        css = generate_minimal_css(infos)
        
        # Check solid-specific font face
        assert "Font Awesome 7 Free" in css
        assert 'src: url("fa-solid-900.woff2");' in css
        assert "font-weight: 900;" in css
        
        # Check individual icon rules
        assert ".fa-house" in css
        assert ".fa-users" in css
        
        # Should not contain brand styles
        assert "Font Awesome 7 Brands" not in css
    
    def test_generate_css_with_mixed_families(self):
        """Test generating CSS with mixed icon families."""
        infos = [
            IconInfo("brands", "github", '.fa-github {\n  --fa: "\\f09b";\n}', "\\f09b"),
            IconInfo("solid", "house", '.fa-house {\n  --fa: "\\f015";\n}', "\\f015")
        ]
        
        css = generate_minimal_css(infos)
        
        # Should contain both brand and solid styles
        assert "Font Awesome 7 Brands" in css
        assert "Font Awesome 7 Free" in css
        assert 'src: url("fa-brands-400.woff2");' in css
        assert 'src: url("fa-solid-900.woff2");' in css
        
        # Check that icons are grouped by family in individual definitions section
        individual_section = css.find("/* Individual icon definitions */")
        assert individual_section != -1
        
        # Look for family sections after the individual definitions marker
        brands_section = css.find("/* Brands icons */", individual_section)
        solid_section = css.find("/* Solid icons */", individual_section)
        
        assert brands_section != -1
        assert solid_section != -1
        
        # GitHub should be in brands section, house in solid section
        github_pos = css.find(".fa-github")
        house_pos = css.find(".fa-house")
        
        # Brands should come before solid in individual icon definitions
        assert brands_section < github_pos
        assert solid_section < house_pos
        assert brands_section < solid_section  # brands section should come before solid section
    
    def test_generate_css_icons_sorted(self):
        """Test that icons are sorted alphabetically within families."""
        infos = [
            IconInfo("brands", "zulu", '.fa-zulu {\n  --fa: "\\f000";\n}', "\\f000"),
            IconInfo("brands", "alpha", '.fa-alpha {\n  --fa: "\\f001";\n}', "\\f001"),
            IconInfo("solid", "zebra", '.fa-zebra {\n  --fa: "\\f002";\n}', "\\f002"),
            IconInfo("solid", "apple", '.fa-apple {\n  --fa: "\\f003";\n}', "\\f003")
        ]
        
        css = generate_minimal_css(infos)
        
        # Find positions of icon rules
        alpha_pos = css.find(".fa-alpha")
        zulu_pos = css.find(".fa-zulu")
        apple_pos = css.find(".fa-apple")
        zebra_pos = css.find(".fa-zebra")
        
        # Within brands section: alpha should come before zulu
        assert alpha_pos < zulu_pos
        
        # Within solid section: apple should come before zebra
        assert apple_pos < zebra_pos


class TestFontGeneration:
    """Test cases for font generation functionality."""
    
    def test_unicode_string_to_codepoint(self):
        """Test converting unicode strings to codepoints."""
        test_cases = [
            ('\\f09b', 61595),  # GitHub icon
            ('\\f015', 61461),  # House icon  
            ('\\e671', 58993),  # Bluesky icon
            ('\\f1d4', 61908),  # Hacker News icon
        ]
        
        for unicode_str, expected_codepoint in test_cases:
            result = unicode_string_to_codepoint(unicode_str)
            assert result == expected_codepoint
    
    def test_extract_codepoints_by_family(self):
        """Test extracting codepoints grouped by family."""
        infos = [
            IconInfo("brands", "github", '.fa-github {\n  --fa: "\\f09b";\n}', "\\f09b"),
            IconInfo("brands", "instagram", '.fa-instagram {\n  --fa: "\\f16d";\n}', "\\f16d"),
            IconInfo("solid", "house", '.fa-house {\n  --fa: "\\f015";\n}', "\\f015"),
        ]
        
        result = extract_codepoints_by_family(infos)
        
        assert "brands" in result
        assert "solid" in result
        assert len(result["brands"]) == 2
        assert len(result["solid"]) == 1
        
        # Check actual codepoints
        assert 61595 in result["brands"]  # GitHub (\f09b)
        assert 61805 in result["brands"]  # Instagram (\f16d)
        assert 61461 in result["solid"]   # House (\f015)
    
    def test_subset_font_file_brands(self):
        """Test creating a font subset with brand icons."""
        # Skip if font files don't exist
        if not Path("fa-brands-400.woff2").exists():
            pytest.skip("Source font files not available")
        
        codepoints = {61595, 61805}  # GitHub and Instagram
        output_path = "test_brands_subset.woff2"
        
        try:
            subset_font_file("fa-brands-400.woff2", codepoints, output_path)
            
            # Verify the output file was created and is smaller than original
            assert Path(output_path).exists()
            
            original_size = Path("fa-brands-400.woff2").stat().st_size
            subset_size = Path(output_path).stat().st_size
            
            assert subset_size < original_size
            assert subset_size > 0  # Should have some content
            
        finally:
            # Clean up
            if Path(output_path).exists():
                Path(output_path).unlink()
    
    def test_subset_font_file_nonexistent(self):
        """Test that subsetting nonexistent font raises FileNotFoundError."""
        with pytest.raises(FileNotFoundError):
            subset_font_file("nonexistent.woff2", {61595}, "output.woff2")
    
    def test_generate_minimal_font_single_family(self):
        """Test generating minimal font with single family."""
        # Skip if font files don't exist
        if not Path("fa-brands-400.woff2").exists():
            pytest.skip("Source font files not available")
        
        infos = [
            IconInfo("brands", "github", '.fa-github {\n  --fa: "\\f09b";\n}', "\\f09b"),
            IconInfo("brands", "instagram", '.fa-instagram {\n  --fa: "\\f16d";\n}', "\\f16d"),
        ]
        
        output_path = "test_minimal_single.woff2"
        
        try:
            generate_minimal_font(infos, output_path)
            
            # Verify the output file was created
            assert Path(output_path).exists()
            
            # Should be smaller than original
            original_size = Path("fa-brands-400.woff2").stat().st_size
            minimal_size = Path(output_path).stat().st_size
            
            assert minimal_size < original_size
            assert minimal_size > 0
            
        finally:
            # Clean up
            if Path(output_path).exists():
                Path(output_path).unlink()
    
    def test_generate_minimal_font_empty(self):
        """Test that generating font with no icons raises ValueError."""
        with pytest.raises(ValueError, match="No icons provided"):
            generate_minimal_font([], "output.woff2")
    
    def test_generate_minimal_font_multi_family(self):
        """Test generating minimal font with multiple families contains all icons."""
        # Skip if font files don't exist
        if not (Path("fa-brands-400.woff2").exists() and Path("fa-solid-900.woff2").exists()):
            pytest.skip("Source font files not available")
        
        infos = [
            IconInfo("brands", "github", '.fa-github {\n  --fa: "\\f09b";\n}', "\\f09b"),
            IconInfo("brands", "instagram", '.fa-instagram {\n  --fa: "\\f16d";\n}', "\\f16d"),
            IconInfo("solid", "house", '.fa-house {\n  --fa: "\\f015";\n}', "\\f015"),
            IconInfo("solid", "users", '.fa-users {\n  --fa: "\\f0c0";\n}', "\\f0c0"),
        ]
        
        output_path = "test_minimal_multi.woff2"
        
        try:
            generate_minimal_font(infos, output_path)
            
            # Verify the output file was created
            assert Path(output_path).exists()
            
            # Load the font and verify it contains all expected codepoints
            from fontTools.ttLib import TTFont
            font = TTFont(output_path)
            cmap = font['cmap'].getBestCmap()
            
            expected_codepoints = {61595, 61805, 61461, 61632}  # github, instagram, house, users
            found_codepoints = set(cmap.keys())
            
            # Check that all expected codepoints are present
            assert expected_codepoints.issubset(found_codepoints), f"Missing codepoints: {expected_codepoints - found_codepoints}"
            
            # Should have exactly the expected number of icons
            assert len(found_codepoints) >= len(expected_codepoints)
            
            font.close()
            
        finally:
            # Clean up
            if Path(output_path).exists():
                Path(output_path).unlink()