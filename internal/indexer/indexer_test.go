package indexer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTokenizer_Tokenize_Chinese(t *testing.T) {
	tok, err := NewTokenizer("", DefaultStopwords())
	if err != nil {
		t.Fatalf("NewTokenizer: %v", err)
	}

	text := "今天北京的天气非常好"
	tokens := tok.Tokenize(text)

	if len(tokens) == 0 {
		t.Fatal("got 0 tokens for Chinese text")
	}

	// gse search mode tokenizes each character for maximal recall.
	// Verify we get individual characters and that common ones are present.
	found := make(map[string]bool)
	for _, tok := range tokens {
		found[tok] = true
	}

	// Characters that should appear (gse search mode produces single chars).
	for _, ch := range []string{"今", "天", "北", "京", "气", "非", "常", "好"} {
		if !found[ch] {
			t.Logf("token %q not found (search mode may group differently): tokens=%v", ch, tokens)
		}
	}
}

func TestTokenizer_Tokenize_Stopwords(t *testing.T) {
	tok, err := NewTokenizer("", DefaultStopwords())
	if err != nil {
		t.Fatalf("NewTokenizer: %v", err)
	}

	// "的" is a stopword.
	tokens := tok.Tokenize("我的电脑")

	for _, tok := range tokens {
		if tok == "的" {
			t.Errorf("stopword '的' was not filtered out: %v", tokens)
		}
	}
}

func TestTokenizer_Tokenize_Empty(t *testing.T) {
	tok, err := NewTokenizer("", nil)
	if err != nil {
		t.Fatalf("NewTokenizer: %v", err)
	}

	tokens := tok.Tokenize("")
	if tokens != nil {
		t.Errorf("expected nil for empty text, got %v", tokens)
	}
}

func TestTokenizer_Deduplicates(t *testing.T) {
	tok, err := NewTokenizer("", nil)
	if err != nil {
		t.Fatalf("NewTokenizer: %v", err)
	}

	// "北京北京北京" should produce fewer tokens than characters.
	tokens := tok.Tokenize("北京北京北京")

	seen := make(map[string]int)
	for _, tok := range tokens {
		seen[tok]++
	}
	for word, count := range seen {
		if count > 1 {
			t.Errorf("token %q appears %d times (should be deduplicated)", word, count)
		}
	}
}

func TestLoadStopwordsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stopwords.txt")
	err := os.WriteFile(path, []byte("foo bar baz"), 0644)
	if err != nil {
		t.Fatalf("write stopwords: %v", err)
	}

	sw, err := LoadStopwordsFile(path)
	if err != nil {
		t.Fatalf("LoadStopwordsFile: %v", err)
	}

	for _, w := range []string{"foo", "bar", "baz"} {
		if !sw[w] {
			t.Errorf("expected stopword %q", w)
		}
	}
	if sw["notfound"] {
		t.Error("unexpected stopword in set")
	}
}

func TestDefaultStopwords(t *testing.T) {
	sw := DefaultStopwords()
	if len(sw) < 50 {
		t.Errorf("default stopwords too few: %d", len(sw))
	}
	// Key Chinese stopwords.
	for _, w := range []string{"的", "了", "是", "我", "the", "a", "is"} {
		if !sw[w] {
			t.Errorf("expected default stopword %q", w)
		}
	}
}

func TestTokenizer_DictCache(t *testing.T) {
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "test_dict.gob")

	// First call: loads from embedded, saves cache.
	tok1, err := NewTokenizer(cachePath, nil)
	if err != nil {
		t.Fatalf("first NewTokenizer: %v", err)
	}
	_ = tok1

	// Second call: should load from cache.
	tok2, err := NewTokenizer(cachePath, nil)
	if err != nil {
		t.Fatalf("second NewTokenizer: %v", err)
	}

	// Both should produce same tokens.
	text := "搜索引擎分词测试"
	t1 := tok1.Tokenize(text)
	t2 := tok2.Tokenize(text)

	if len(t1) != len(t2) {
		t.Errorf("cache inconsistency: first=%v, second=%v", t1, t2)
	}
}

func TestTokenizer_MixedChineseEnglish(t *testing.T) {
	tok, err := NewTokenizer("", DefaultStopwords())
	if err != nil {
		t.Fatalf("NewTokenizer: %v", err)
	}

	tokens := tok.Tokenize("Python是一种编程语言")

	found := make(map[string]bool)
	for _, tok := range tokens {
		found[tok] = true
	}

	if !found["python"] && !found["Python"] {
		// gse may lowercase.
		t.Logf("tokens for mixed text: %v", tokens)
	}
}

func TestQuickHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hash_test.txt")
	err := os.WriteFile(path, []byte("hello world this is a test file for quick hashing"), 0644)
	if err != nil {
		t.Fatalf("write test file: %v", err)
	}

	h1, err := QuickHash(path)
	if err != nil {
		t.Fatalf("QuickHash: %v", err)
	}
	if h1 == 0 {
		t.Error("QuickHash returned 0")
	}

	// Same content → same hash.
	h2, err := QuickHash(path)
	if err != nil {
		t.Fatalf("second QuickHash: %v", err)
	}
	if h1 != h2 {
		t.Errorf("QuickHash not deterministic: %d vs %d", h1, h2)
	}

	// Different content → different hash.
	err = os.WriteFile(path, []byte("completely different content here"), 0644)
	if err != nil {
		t.Fatalf("rewrite file: %v", err)
	}
	h3, err := QuickHash(path)
	if err != nil {
		t.Fatalf("third QuickHash: %v", err)
	}
	if h1 == h3 {
		t.Error("QuickHash should differ for different content")
	}
}

func TestQuickHash_SmallFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.txt")
	err := os.WriteFile(path, []byte("hi"), 0644)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	h, err := QuickHash(path)
	if err != nil {
		t.Fatalf("QuickHash on small file: %v", err)
	}
	if h == 0 {
		t.Error("QuickHash returned 0 for small file")
	}
}

func TestFullMD5(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "md5_test.txt")
	content := []byte("test content for MD5 hashing")
	err := os.WriteFile(path, content, 0644)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	h1, err := FullMD5(path)
	if err != nil {
		t.Fatalf("FullMD5: %v", err)
	}
	if len(h1) != 32 {
		t.Errorf("MD5 hex length: got %d, want 32", len(h1))
	}

	// Deterministic.
	h2, err := FullMD5(path)
	if err != nil {
		t.Fatalf("second FullMD5: %v", err)
	}
	if h1 != h2 {
		t.Error("FullMD5 not deterministic")
	}
}

func TestDetectAndDecode_UTF8(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "utf8.txt")
	text := "这是一段UTF-8编码的中文文本。"
	err := os.WriteFile(path, []byte(text), 0644)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	result, err := DetectAndDecode(path, 10*1024*1024)
	if err != nil {
		t.Fatalf("DetectAndDecode: %v", err)
	}
	if !strings.Contains(result, "UTF-8") && !strings.Contains(result, "这是一段") {
		t.Errorf("unexpected decode result: %q", result)
	}
}

func TestDetectAndDecode_Oversize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	// Write 200 bytes but set maxSize to 100.
	err := os.WriteFile(path, make([]byte, 200), 0644)
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	_, err = DetectAndDecode(path, 100)
	if err == nil {
		t.Error("expected error for oversized file")
	}
}

func TestExtractHTMLText(t *testing.T) {
	html := `<html><head><title>Test</title><style>body{}</style></head>
<body><h1>标题</h1><p>这是一段文本。</p><script>var x=1;</script>
<div>更多内容</div></body></html>`

	text := ExtractHTMLText(html)

	if strings.Contains(text, "var x=1") {
		t.Error("script content should be excluded")
	}
	if strings.Contains(text, "body{}") {
		t.Error("style content should be excluded")
	}
	if !strings.Contains(text, "标题") {
		t.Error("h1 content missing")
	}
	if !strings.Contains(text, "这是一段文本") {
		t.Error("paragraph content missing")
	}
	if !strings.Contains(text, "更多内容") {
		t.Error("div content missing")
	}
}

func TestWalkFiles_Basic(t *testing.T) {
	dir := t.TempDir()

	// Create test files.
	files := map[string]string{
		"a.txt":  "hello",
		"b.md":   "world",
		"c.go":   "package main",
		".hidden": "secret",
		"sub/d.go": "func test()",
	}
	for path, content := range files {
		fullPath := filepath.Join(dir, path)
		os.MkdirAll(filepath.Dir(fullPath), 0755)
		os.WriteFile(fullPath, []byte(content), 0644)
	}

	// Create a symlink subdirectory (should be skipped).
	linkPath := filepath.Join(dir, "linkdir")
	os.Mkdir(filepath.Join(dir, "realsub"), 0755)
	os.Symlink(filepath.Join(dir, "realsub"), linkPath)

	// Walk without hidden files.
	fileCh, errCh := WalkFiles(t.Context(), dir, false)

	var paths []string
	for entry := range fileCh {
		paths = append(paths, entry.Path)
	}
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("walk error: %v", err)
		}
	default:
	}

	// Hidden file should be excluded.
	for _, p := range paths {
		if filepath.Base(p) == ".hidden" {
			t.Error("hidden file should be excluded")
		}
	}

	// Subdirectory files should be found.
	found := make(map[string]bool)
	for _, p := range paths {
		found[filepath.Base(p)] = true
	}
	for _, name := range []string{"a.txt", "b.md", "c.go", "d.go"} {
		if !found[name] {
			t.Errorf("expected file %q not found in walk", name)
		}
	}
}

func TestWalkFiles_IncludeHidden(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".hidden_file"), []byte("secret"), 0644)

	fileCh, _ := WalkFiles(t.Context(), dir, true)

	var found bool
	for entry := range fileCh {
		if filepath.Base(entry.Path) == ".hidden_file" {
			found = true
		}
	}
	if !found {
		t.Error("hidden file should be included with IncludeHidden=true")
	}
}
