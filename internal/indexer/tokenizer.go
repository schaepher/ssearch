package indexer

import (
	"encoding/gob"
	"fmt"
	"os"
	"strings"

	"github.com/go-ego/gse"
)

const dictVersion = "v6.0"

// Tokenizer wraps the gse segmenter with stopword filtering.
type Tokenizer struct {
	seg       *gse.Segmenter
	stopwords map[string]bool
}

// NewTokenizer creates a Tokenizer. It attempts to load a cached .gob
// dictionary if available; otherwise loads the embedded gse dictionary
// and persists the cache.
func NewTokenizer(dictCachePath string, stopwords map[string]bool) (*Tokenizer, error) {
	seg := &gse.Segmenter{}

	// Try loading from gob cache first.
	if loaded := loadDictCache(seg, dictCachePath); !loaded {
		// Load embedded dictionary.
		if err := seg.LoadDictEmbed("zh"); err != nil {
			// Try loading default dict from gse's embedded data.
			return nil, fmt.Errorf("load gse dict: %w", err)
		}

		// Persist the cache for next time.
		saveDictCache(seg, dictCachePath)
	}

	return &Tokenizer{
		seg:       seg,
		stopwords: stopwords,
	}, nil
}

// loadDictCache attempts to deserialize the segmenter from a .gob file.
// Returns true on success.
func loadDictCache(seg *gse.Segmenter, path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	// Read version header.
	var ver string
	if err := gob.NewDecoder(f).Decode(&ver); err != nil {
		return false
	}
	if ver != dictVersion {
		return false
	}

	// Decode the segmenter.
	if err := gob.NewDecoder(f).Decode(seg); err != nil {
		return false
	}
	return true
}

// saveDictCache serializes the segmenter to a .gob file for fast startup.
func saveDictCache(seg *gse.Segmenter, path string) {
	f, err := os.Create(path)
	if err != nil {
		return // best-effort; silently skip
	}
	defer f.Close()

	enc := gob.NewEncoder(f)
	// Write version header.
	if err := enc.Encode(dictVersion); err != nil {
		return
	}
	// Encode the segmenter.
	_ = enc.Encode(seg)
}

// Tokenize segments text using gse search mode, filters stopwords,
// and returns deduplicated tokens.
func (t *Tokenizer) Tokenize(text string) []string {
	if t.seg == nil || text == "" {
		return nil
	}

	// Use Slice in search mode for maximum keyword recall.
	words := t.seg.Slice(text, true)

	seen := make(map[string]struct{}, len(words))
	result := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		// Filter stopwords.
		if t.stopwords != nil && t.stopwords[w] {
			continue
		}
		// Deduplicate.
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		result = append(result, w)
	}
	return result
}

// TokenizePrecise segments text using gse precise mode (non-search),
// preserving dictionary compounds that match the full input.
// Use this for search query tokenization to avoid splitting short
// compound words like "自旋" into individual characters.
func (t *Tokenizer) TokenizePrecise(text string) []string {
	if t.seg == nil || text == "" {
		return nil
	}

	words := t.seg.Slice(text, false)

	seen := make(map[string]struct{}, len(words))
	result := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		if t.stopwords != nil && t.stopwords[w] {
			continue
		}
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		result = append(result, w)
	}
	return result
}

// DefaultStopwords returns a built-in set of Chinese and English stopwords.
func DefaultStopwords() map[string]bool {
	sw := []string{
		// Chinese stopwords
		"的", "了", "在", "是", "我", "有", "和", "就", "不", "人",
		"都", "一", "一个", "上", "也", "很", "到", "说", "要", "去",
		"你", "会", "着", "没有", "看", "好", "自己", "这", "他", "她",
		"它", "们", "那", "这个", "那个", "什么", "怎么", "哪", "为什么",
		"可以", "还是", "只是", "但是", "如果", "因为", "所以", "而且",
		"虽然", "然后", "已经", "还", "又", "再", "才", "刚", "将",
		"把", "被", "让", "给", "从", "以", "对", "向", "往", "朝",
		"与", "同", "跟", "当", "为", "为了", "按照", "通过", "经过",
		"比", "如", "例如", "等", "等等", "或", "或者", "并", "并且",
		"而", "但", "却", "只", "只有", "无论", "不管", "除了",
		// Common English stopwords
		"the", "a", "an", "is", "are", "was", "were", "be", "been",
		"being", "have", "has", "had", "do", "does", "did", "will",
		"would", "could", "should", "may", "might", "can", "shall",
		"i", "you", "he", "she", "it", "we", "they", "me", "him",
		"her", "us", "them", "my", "your", "his", "its", "our",
		"their", "this", "that", "these", "those", "am", "not",
		"no", "nor", "or", "and", "but", "if", "then", "else",
		"when", "where", "how", "what", "which", "who", "whom",
		"to", "of", "in", "for", "on", "with", "at", "by", "from",
		"as", "into", "through", "during", "before", "after",
		"above", "below", "between", "under", "again", "further",
		"once", "here", "there", "all", "both", "each", "few",
		"more", "most", "other", "some", "such", "only", "own",
		"same", "so", "than", "too", "very", "just", "about",
	}
	m := make(map[string]bool, len(sw))
	for _, w := range sw {
		m[w] = true
	}
	return m
}

// LoadStopwordsFile reads stopwords from a file (one per line or
// whitespace-separated).
func LoadStopwordsFile(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read stopwords file: %w", err)
	}
	fields := strings.Fields(string(data))
	m := make(map[string]bool, len(fields))
	for _, w := range fields {
		w = strings.TrimSpace(w)
		if w != "" {
			m[w] = true
		}
	}
	return m, nil
}
