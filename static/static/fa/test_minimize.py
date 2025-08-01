#!/usr/bin/env python3
"""
Tests for the minimize.py FontAwesome minimization tool.
"""

import pytest
import tempfile
import os
from pathlib import Path
from minimize import parse_spec_file


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