package utils

import "strings"

// SupportedExtensions returns the built-in list of file extensions
// that ssearch will attempt to index.
func SupportedExtensions() []string {
	return []string{
		".txt", ".md", ".log", ".csv", ".json", ".xml",
		".html", ".htm", ".py", ".js", ".ts", ".go",
		".java", ".c", ".cpp", ".h", ".hpp", ".rs",
		".yaml", ".yml", ".toml", ".cfg", ".ini", ".conf",
		".css", ".sql", ".sh", ".bat", ".ps1",
		".pdf.txt", // OCR output
		".rst", ".tex", ".org",
	}
}

// IsSupportedExt returns true if the file extension is in the supported list.
func IsSupportedExt(path string, extraExts []string) bool {
	ext := strings.ToLower(path[strings.LastIndex(path, "."):])
	for _, e := range SupportedExtensions() {
		if e == ext {
			return true
		}
	}
	for _, e := range extraExts {
		if strings.ToLower(e) == ext {
			return true
		}
	}
	return false
}

// IsHidden returns true if name starts with ".".
func IsHidden(name string) bool {
	return len(name) > 0 && name[0] == '.'
}

// ShouldSkip returns true if the file or directory name should always be
// skipped during file tree traversal.
func ShouldSkip(name string) bool {
	// Self-protection: skip .ssearch directory, database files, lock files, temp files.
	switch name {
	case ".ssearch", "data.db", ".lock":
		return true
	}
	// Skip temp files.
	if strings.HasSuffix(name, ".tmp") || strings.HasSuffix(name, ".temp") {
		return true
	}
	return false
}

// ShouldSkipFile returns true if the basename indicates a file that
// should never be indexed.
func ShouldSkipFile(name string) bool {
	return ShouldSkip(name)
}
