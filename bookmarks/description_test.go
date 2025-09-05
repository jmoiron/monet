package bookmarks

import (
	"bytes"
	"testing"
)

func TestGetDescriptionFromReader(t *testing.T) {
	tests := []struct {
		name        string
		jsonContent string
		expected    string
		wantErr     bool
	}{
		{
			name:        "with meta description",
			jsonContent: `{"html": "<html><head><meta name=\"description\" content=\"This is a test description\"></head></html>"}`,
			expected:    "This is a test description",
			wantErr:     false,
		},
		{
			name:        "with meta description and other attributes",
			jsonContent: `{"html": "<html><head><meta name=\"description\" content=\"Description with extras\" data-preact-helmet=\"true\"></head></html>"}`,
			expected:    "Description with extras",
			wantErr:     false,
		},
		{
			name:        "with meta description in different attribute order",
			jsonContent: `{"html": "<html><head><meta content=\"Different order description\" name=\"description\"></head></html>"}`,
			expected:    "Different order description",
			wantErr:     false,
		},
		{
			name:        "without meta description",
			jsonContent: `{"html": "<html><head><meta name=\"viewport\" content=\"width=device-width\"><title>Test</title></head></html>"}`,
			expected:    "",
			wantErr:     false,
		},
		{
			name:        "with empty meta description",
			jsonContent: `{"html": "<html><head><meta name=\"description\" content=\"\"></head></html>"}`,
			expected:    "",
			wantErr:     false,
		},
		{
			name:        "with whitespace in description",
			jsonContent: `{"html": "<html><head><meta name=\"description\" content=\"  Whitespace description  \"></head></html>"}`,
			expected:    "Whitespace description",
			wantErr:     false,
		},
		{
			name:        "invalid json",
			jsonContent: `{"html": invalid json}`,
			expected:    "",
			wantErr:     true,
		},
		{
			name:        "missing html field",
			jsonContent: `{"other_field": "value"}`,
			expected:    "",
			wantErr:     false,
		},
		{
			name:        "malformed html",
			jsonContent: `{"html": "<html><head><meta name=\"description\" content=\"Valid description\"><invalid></head></html>"}`,
			expected:    "Valid description",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bytes.NewBufferString(tt.jsonContent)
			
			result, err := GetDescriptionFromReader(reader)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetDescriptionFromReader() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && result != tt.expected {
				t.Errorf("Expected description '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestGetDescriptionFromPath_NonexistentFile(t *testing.T) {
	// Test with a nonexistent file
	jsonPath := "/tmp/nonexistent-test-file.json"
	
	description, err := GetDescriptionFromPath(jsonPath)
	if err != nil {
		t.Fatalf("GetDescriptionFromPath should not error on nonexistent file: %v", err)
	}

	if description != "" {
		t.Errorf("Expected empty description for nonexistent file, got '%s'", description)
	}

	t.Logf("Correctly handled nonexistent file")
}