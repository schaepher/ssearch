# ssearch

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**ssearch** is a cross-platform, single-binary CLI full-text search tool with Chinese word segmentation. It builds an inverted index over local text files and delivers millisecond keyword-to-path retrieval — no daemon, no external services, just one file.

## Features

- **Chinese word segmentation** via [gse](https://github.com/go-ego/gse) (search mode), with built-in Chinese + English stopwords
- **Inverted index** backed by [BoltDB](https://github.com/etcd-io/bbolt) — embedded, ACID, single-file
- **Smart incremental updates** — daily `update` checks file sizes only; MD5 hash is computed only on size collision or when `--deep` is requested
- **Encoding auto-detection** — [chardet](https://github.com/saintfish/chardet) with UTF-8 fallback; problematic files are queued for retry
- **HTML-aware parsing** — extracts visible text from `.html`/`.htm` files, skipping `<script>` and `<style>`
- **Duplicate detection** — standalone MD5-based `dedup` subcommand
- **Zero configuration** — auto-discovers project root by walking up from CWD looking for `.ssearch/`

## Quick Start

```bash
# 1. Build the index (first time — may take a while on large trees)
ssearch index

# 2. Search
ssearch search 关键词1 关键词2

# 3. Daily refresh (fast — reads only file sizes)
ssearch update
```

## Installation

### Download binary

Grab the latest release from the [Releases](https://github.com/user/ssearch/releases) page. Available for Windows, macOS, and Linux.

### Build from source

```bash
git clone https://github.com/user/ssearch.git
cd ssearch
go build -o ssearch ./cmd/ssearch/
```

Requires **Go 1.25+**. No CGO, no system dependencies.

## Commands

| Command | What it does |
|---|---|
| `ssearch index` | Full rebuild — clears the database and re-indexes every supported file |
| `ssearch update` | Incremental update — picks up new, changed, and deleted files (daily driver) |
| `ssearch search <keywords...>` | Multi-keyword **AND** search, sorted newest-first |
| `ssearch dedup` | Find duplicate files by full MD5 (independent of the index) |
| `ssearch pending` | View or clear files that failed to index |
| `ssearch dead` | View or retry permanently-skipped files |
| `ssearch compact` | Shrink the database file after large updates or deletions |

Add `--help` to any command for the full flag reference.

### Global flags

| Flag | Default | Description |
|---|---|---|
| `--db <path>` | `<data-dir>/data.db` | Override database file path |
| `--include-hidden` | `false` | Include files and directories starting with `.` |

### `ssearch index`

```
ssearch index [--root <dir>] [--max-size 50MB] [--stopwords <file>]
```

| Flag | Default | Description |
|---|---|---|
| `--root` | auto-detected | Project root to scan |
| `--max-size` | `50MB` | Skip files larger than this (goes to pending) |
| `--stopwords` | built-in | Path to custom stopwords file |

### `ssearch update`

```
ssearch update [--root <dir>] [--deep] [--force] [--force-pending] [--filter <path>] [--max-size 50MB]
```

| Flag | Description |
|---|---|
| `--deep` | Always compute a QuickHash (4 KB prefix) to detect content changes even when size is unchanged |
| `--force` | Unconditionally re-index every existing file |
| `--force-pending` | Retry all files in the pending queue |
| `--filter` | Restrict update to a single file or directory |

### `ssearch search`

```
ssearch search <keyword1> [keyword2...] [--limit 100]
```

- Keywords are tokenized with Chinese word segmentation before matching
- **AND** semantics: a file must contain every keyword
- Results are printed as `ModTime<TAB>Path<TAB>Size`, sorted newest-first
- `--limit 0` returns all matches

### `ssearch dedup`

```
ssearch dedup [--root <dir>]
```

Walks the file tree independently of the index and groups files by full MD5 hash. Only groups with 2+ files are printed.

### `ssearch pending`

```
ssearch pending --list
ssearch pending --clear
```

Files that failed to index (encoding errors, lock contention, oversized) are recorded here. Use `ssearch update --force-pending` to retry.

### `ssearch dead`

```
ssearch dead --list
ssearch dead --retry <path>
```

After 3 failed retries, a pending file moves to the dead list. Use `--retry` to move it back to the pending queue.

### `ssearch compact`

```
ssearch compact
```

Copies live data to a new database file and replaces the original, reclaiming space from deleted or re-indexed files.

## How it works

### Directory auto-discovery

When you run any command, ssearch walks **up** from your current working directory looking for a hidden `.ssearch/` folder:

```
~/projects/my-blog/
├── .ssearch/          ← found here
│   ├── data.db
│   ├── .mysearch_dict.gob
│   └── .mysearch.toml
├── posts/
├── images/
└── ...
```

If no `.ssearch/` directory is found anywhere up to the filesystem root, one is created in your CWD.

### Index pipeline

```
WalkDir → chardet → UTF-8 → Tokenize (gse) → Inverted Index (BoltDB)
                  ↘ fail → pending (retry ×3 → dead)
```

### Incremental update logic

| Condition | Action |
|---|---|
| New file | Full index |
| Size same, ModTime same | Skip |
| Size same, ModTime changed | Update ModTime only (no re-index) |
| Size same, `--deep` + QuickHash match | Skip |
| Size same, `--deep` + QuickHash differs + MD5 match | Update Size only |
| Size changed, QuickHash match | Update Size only |
| Size changed, QuickHash differs + MD5 match | Update Size only |
| Size changed, QuickHash differs + MD5 differs | **Re-index** |
| `--force` | Re-index unconditionally |

### Database schema

| Bucket | Key → Value | Purpose |
|---|---|---|
| `meta` | string → string | `version`, `root`, `nextFileId` |
| `pathmap` | uint64 → string | File ID → absolute path |
| `rpathmap` | string → uint64 | Absolute path → file ID |
| `filemeta` | uint64 → JSON | `{Size, MD5, QuickHash, ModTime, RetryCount}` |
| `inverted` | term → sub-bucket (ID→∅) | Inverted index: term → set of file IDs |
| `pending` | path → JSON | Files awaiting retry |
| `dead` | path → JSON | Files permanently skipped |

## Configuration

Optional `.ssearch/.mysearch.toml`:

```toml
[search]
# Append additional file extensions ('.txt', '.md', etc. are built-in)
extensions = [".org", ".rst", ".adoc"]

# Override built-in stopwords
stopwords = ["foo", "bar", "baz"]

# Or load stopwords from a file
stopwords_file = "/path/to/stopwords.txt"

# Override default max file size
max_size = "100MB"
```

If the file is absent, sensible defaults are used.

## Supported file types

`.txt` `.md` `.log` `.csv` `.json` `.xml` `.html` `.htm` `.py` `.js` `.ts` `.go` `.java` `.c` `.cpp` `.h` `.hpp` `.rs` `.yaml` `.yml` `.toml` `.cfg` `.ini` `.conf` `.css` `.sql` `.sh` `.bat` `.ps1` `.rst` `.tex` `.org` `.pdf.txt`

Extend this list via the TOML config file.

## Performance

| Operation | Scale | Expected time |
|---|---|---|
| Full index (first run) | ~100K files | >30 min |
| Incremental update | ~100K files | 1–3 sec |
| Search | Any size | <100 ms |
| Memory (steady state) | — | <200 MB |

## License

MIT
