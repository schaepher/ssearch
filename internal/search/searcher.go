package search

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"go.etcd.io/bbolt"

	"ssearch/internal/storage"
)

// Result holds one search hit.
type Result struct {
	Path    string
	ModTime time.Time
	Size    int64
}

// Searcher performs AND searches over the inverted index.
type Searcher struct {
	bdb *bbolt.DB
}

// New creates a Searcher backed by the given database.
func New(bdb *bbolt.DB) *Searcher {
	return &Searcher{bdb: bdb}
}

// Search performs AND intersection of inverted-index lists for each term.
func (s *Searcher) Search(terms []string, limit int) ([]Result, []string, error) {
	if len(terms) == 0 {
		return nil, nil, nil
	}

	var results []Result
	var unmatched []string

	err := s.bdb.View(func(tx *bbolt.Tx) error {
		type termList struct {
			term string
			ids  []uint64
		}
		lists := make([]termList, 0, len(terms))

		for _, term := range terms {
			ids, err := storage.FileIDsForTerm(tx, term)
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				unmatched = append(unmatched, term)
				continue
			}
			lists = append(lists, termList{term: term, ids: ids})
		}

		for _, t := range unmatched {
			fmt.Fprintf(os.Stderr, "词 [%s] 无匹配\n", t)
		}

		if len(lists) == 0 {
			return nil
		}

		// Sort by ascending length for optimal intersection.
		sort.Slice(lists, func(i, j int) bool {
			return len(lists[i].ids) < len(lists[j].ids)
		})

		// Build base set from shortest list.
		set := make(map[uint64]struct{}, len(lists[0].ids))
		for _, id := range lists[0].ids {
			set[id] = struct{}{}
		}

		// Intersect with remaining lists.
		for _, tl := range lists[1:] {
			termSet := make(map[uint64]struct{}, len(tl.ids))
			for _, id := range tl.ids {
				termSet[id] = struct{}{}
			}
			for id := range set {
				if _, ok := termSet[id]; !ok {
					delete(set, id)
				}
			}
			if len(set) == 0 {
				return nil
			}
		}

		if len(set) == 0 {
			return nil
		}

		// Assemble results.
		results = make([]Result, 0, len(set))
		pathmap := tx.Bucket(storage.BucketPathmap)
		filemeta := tx.Bucket(storage.BucketFileMeta)
		for id := range set {
			pathBytes := pathmap.Get(storage.Uint64ToBytes(id))
			if pathBytes == nil {
				continue
			}
			fmBytes := filemeta.Get(storage.Uint64ToBytes(id))
			if fmBytes == nil {
				continue
			}
			var fm storage.FileMeta
			if err := json.Unmarshal(fmBytes, &fm); err != nil {
				continue
			}

			results = append(results, Result{
				Path:    string(pathBytes),
				ModTime: fm.ModTime,
				Size:    fm.Size,
			})
		}

		sort.Slice(results, func(i, j int) bool {
			return results[i].ModTime.After(results[j].ModTime)
		})

		if limit > 0 && len(results) > limit {
			results = results[:limit]
		}

		return nil
	})

	return results, unmatched, err
}
