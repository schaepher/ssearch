package indexer

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/saintfish/chardet"
	"golang.org/x/net/html"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

// DetectAndDecode reads a file (up to maxSize bytes), detects its encoding,
// converts to UTF-8, and returns the text content. For HTML files, it
// extracts only visible text nodes.
//
// Fallback chain per PRD §5.3:
//  1. chardet detection (confidence ≥ 0.5) → decode with detected charset
//  2. Try raw UTF-8 decode → accept if no replacement characters
//  3. Return error → caller puts file in pending
func DetectAndDecode(path string, maxSize int64) (string, error) {
	// Read file (limited to maxSize).
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	// Read up to maxSize + 1 to detect oversized files.
	lr := io.LimitReader(f, maxSize+1)
	data, err := io.ReadAll(lr)
	if err != nil {
		return "", fmt.Errorf("read: %w", err)
	}
	if int64(len(data)) > maxSize {
		return "", fmt.Errorf("file exceeds max size %d bytes", maxSize)
	}

	// Step 1: chardet detection.
	detector := chardet.NewTextDetector()
	result, err := detector.DetectBest(data)
	if err == nil && result != nil && result.Confidence >= 50 {
		decoded, decErr := decodeBytes(data, result.Charset)
		if decErr == nil {
			return extractText(path, decoded)
		}
		// chardet decode failed; fall through to UTF-8 fallback.
	}

	// Step 2: Try UTF-8 directly.
	decoded := string(data)
	if strings.ToValidUTF8(decoded, "") == decoded {
		return extractText(path, decoded)
	}

	// Step 3: Both failed.
	return "", fmt.Errorf("encoding detection failed: unable to decode as UTF-8 or detected charset")
}

// decodeBytes converts data from the given charset to UTF-8.
func decodeBytes(data []byte, charset string) (string, error) {
	var enc encoding.Encoding

	// Normalize charset name.
	cs := strings.ToLower(strings.TrimSpace(charset))

	switch cs {
	case "utf-8", "utf8":
		// Already UTF-8.
		if strings.ToValidUTF8(string(data), "") != string(data) {
			return "", fmt.Errorf("invalid utf-8")
		}
		return string(data), nil
	case "iso-8859-1", "latin1", "latin-1":
		enc = charmap.ISO8859_1
	case "iso-8859-2", "latin2":
		enc = charmap.ISO8859_2
	case "iso-8859-15":
		enc = charmap.ISO8859_15
	case "windows-1252", "windows1252":
		enc = charmap.Windows1252
	case "gb-18030", "gb18030", "gbk", "gb2312":
		enc = simplifiedchinese.GB18030
	case "big5", "big-5":
		enc = traditionalchinese.Big5
	case "shift_jis", "shift-jis", "sjis":
		enc = japanese.ShiftJIS
	case "euc-jp":
		enc = japanese.EUCJP
	case "euc-kr":
		enc = korean.EUCKR
	default:
		return "", fmt.Errorf("unsupported charset %q", charset)
	}

	decoder := enc.NewDecoder()
	decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(data), decoder))
	if err != nil {
		return "", fmt.Errorf("decode %q: %w", charset, err)
	}
	return string(decoded), nil
}

// extractText handles HTML extraction if the path indicates HTML content;
// otherwise returns the raw text unchanged.
func extractText(path string, raw string) (string, error) {
	ext := strings.ToLower(path)
	if strings.HasSuffix(ext, ".html") || strings.HasSuffix(ext, ".htm") {
		return ExtractHTMLText(raw), nil
	}
	return raw, nil
}

// ExtractHTMLText extracts visible text from HTML content using the
// x/net/html tokenizer. It skips <script> and <style> elements.
func ExtractHTMLText(data string) string {
	r := strings.NewReader(data)
	z := html.NewTokenizer(r)

	var buf strings.Builder
	inSkip := false

	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return buf.String()

		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			tag := strings.ToLower(string(name))
			if tag == "script" || tag == "style" || tag == "noscript" {
				inSkip = true
			}

		case html.EndTagToken:
			name, _ := z.TagName()
			tag := strings.ToLower(string(name))
			if tag == "script" || tag == "style" || tag == "noscript" {
				inSkip = false
			}
			// Add a space after block-level elements.
			switch tag {
			case "p", "div", "br", "li", "h1", "h2", "h3", "h4", "h5", "h6",
				"tr", "td", "th", "section", "article", "header", "footer":
				buf.WriteByte(' ')
			}

		case html.TextToken:
			if !inSkip {
				text := strings.TrimSpace(string(z.Text()))
				if text != "" {
					if buf.Len() > 0 {
						buf.WriteByte(' ')
					}
					buf.WriteString(text)
				}
			}
		}
	}
}
