package utils

import (
	"testing"
)

func TestIsSupportedExt(t *testing.T) {
	tests := []struct {
		path      string
		extraExts []string
		want      bool
	}{
		{"/path/to/file.txt", nil, true},
		{"/path/to/file.md", nil, true},
		{"/path/to/file.go", nil, true},
		{"/path/to/file.py", nil, true},
		{"/path/to/file.html", nil, true},
		{"/path/to/file.htm", nil, true},
		{"/path/to/file.json", nil, true},
		{"/path/to/file.pdf.txt", nil, true}, // OCR output
		{"/path/to/file.png", nil, false},
		{"/path/to/file.jpg", nil, false},
		{"/path/to/file.pdf", nil, false},
		{"/path/to/file.exe", nil, false},
		{"/path/to/file.doc", nil, false},
		{"/path/to/file", nil, false}, // no extension
		{"/path/to/file.xyz", []string{".xyz"}, true},
		{"/path/to/file.xyz", []string{".abc"}, false},
		{"/path/to/.hidden.txt", nil, true}, // hidden files still checked by extension
	}

	for _, tt := range tests {
		got := IsSupportedExt(tt.path, tt.extraExts)
		if got != tt.want {
			t.Errorf("IsSupportedExt(%q, %v) = %v, want %v",
				tt.path, tt.extraExts, got, tt.want)
		}
	}
}

func TestIsHidden(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{".git", true},
		{".hidden", true},
		{".ssearch", true},
		{"normal.txt", false},
		{"README.md", false},
		{"", false},
	}

	for _, tt := range tests {
		got := IsHidden(tt.name)
		if got != tt.want {
			t.Errorf("IsHidden(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestShouldSkip(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{".ssearch", true},
		{"data.db", true},
		{".lock", true},
		{"file.tmp", true},
		{"file.temp", true},
		{"normal.txt", false},
		{"README.md", false},
		{"main.go", false},
	}

	for _, tt := range tests {
		got := ShouldSkip(tt.name)
		if got != tt.want {
			t.Errorf("ShouldSkip(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestSupportedExtensions_NotEmpty(t *testing.T) {
	exts := SupportedExtensions()
	if len(exts) < 20 {
		t.Errorf("only %d supported extensions, expected at least 20", len(exts))
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"500", 500},
		{"1KB", 1000},
		{"1kb", 1000},
		{"1KiB", 1024},
		{"1kib", 1024},
		{"50MB", 50_000_000},
		{"50mb", 50_000_000},
		{"50MiB", 52_428_800},
		{"1GB", 1_000_000_000},
		{"1GiB", 1_073_741_824},
		{"50MB", 50000000},
		{" 10MB ", 10_000_000},
	}

	for _, tt := range tests {
		got, err := ParseSize(tt.input)
		if err != nil {
			t.Errorf("ParseSize(%q) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseSize(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseSize_Errors(t *testing.T) {
	badInputs := []string{"", "abc", "10XY", "MB", "-5MB"}
	for _, input := range badInputs {
		_, err := ParseSize(input)
		if err == nil {
			t.Errorf("ParseSize(%q) should have returned error", input)
		}
	}
}
