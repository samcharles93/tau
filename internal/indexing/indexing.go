package indexing

import (
	"io/fs"
	"path"
	"strings"

	"github.com/blevesearch/bleve/v2"
)

// Doc represents a document to be indexed.
type Doc struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

// Index wraps a Bleve in-memory index.
type Index struct {
	bleveIndex bleve.Index
}

// SearchHit represents a single search result hit.
type SearchHit struct {
	Path      string
	Title     string
	Score     float64
	Fragments map[string][]string
}

// SearchResult represents the collection of search hits.
type SearchResult struct {
	Hits []SearchHit
}

// Build walks the given fs.FS, parses non-Go files, and returns a built in-memory Index.
func Build(fsys fs.FS) (*Index, error) {
	// Create the index mapping with English analyzer as default to support stemming.
	mapping := bleve.NewIndexMapping()
	mapping.DefaultAnalyzer = "en"

	docMapping := bleve.NewDocumentMapping()

	pathField := bleve.NewTextFieldMapping()
	pathField.Store = true
	pathField.Index = true
	pathField.IncludeTermVectors = true
	docMapping.AddFieldMappingsAt("path", pathField)

	titleField := bleve.NewTextFieldMapping()
	titleField.Store = true
	titleField.Index = true
	titleField.Analyzer = "en"
	titleField.IncludeTermVectors = true
	docMapping.AddFieldMappingsAt("title", titleField)

	contentField := bleve.NewTextFieldMapping()
	contentField.Store = true
	contentField.Index = true
	contentField.Analyzer = "en"
	contentField.IncludeTermVectors = true
	docMapping.AddFieldMappingsAt("content", contentField)

	mapping.DefaultMapping = docMapping

	// Create in-memory index
	bIndex, err := bleve.NewMemOnly(mapping)
	if err != nil {
		return nil, err
	}

	idx := &Index{bleveIndex: bIndex}

	// Walk the filesystem and index files
	err = fs.WalkDir(fsys, ".", func(filePath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") {
			return nil
		}

		contentBytes, err := fs.ReadFile(fsys, filePath)
		if err != nil {
			return err
		}

		title := deriveTitle(filePath)
		contentStr := string(contentBytes)
		if len(contentStr) > 50000 {
			contentStr = contentStr[:50000]
		}
		doc := Doc{
			Path:    filePath,
			Title:   title,
			Content: contentStr,
		}

		if err := idx.bleveIndex.Index(filePath, doc); err != nil {
			_ = idx.bleveIndex.Close()
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return idx, nil
}

// Search queries the in-memory index and returns ranked results.
func (idx *Index) Search(query string, limit int) (*SearchResult, error) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return &SearchResult{Hits: []SearchHit{}}, nil
	}

	q := bleve.NewQueryStringQuery(trimmed)
	req := bleve.NewSearchRequest(q)
	req.Size = limit
	req.Highlight = bleve.NewHighlight()
	req.Fields = []string{"path", "title"}

	res, err := idx.bleveIndex.Search(req)
	if err != nil {
		return nil, err
	}

	hits := make([]SearchHit, 0, len(res.Hits))
	for _, hit := range res.Hits {
		var pathStr, titleStr string
		if val, ok := hit.Fields["path"].(string); ok {
			pathStr = val
		}
		if val, ok := hit.Fields["title"].(string); ok {
			titleStr = val
		}

		hits = append(hits, SearchHit{
			Path:      pathStr,
			Title:     titleStr,
			Score:     hit.Score,
			Fragments: hit.Fragments,
		})
	}

	return &SearchResult{Hits: hits}, nil
}

// Close closes the underlying Bleve index.
func (idx *Index) Close() error {
	return idx.bleveIndex.Close()
}

// deriveTitle extracts the title from the file path according to the specified rules.
func deriveTitle(filePath string) string {
	filename := path.Base(filePath)
	title := filename
	if before, ok := strings.CutSuffix(title, ".md"); ok {
		title = before
	} else if before, ok := strings.CutSuffix(title, ".yaml"); ok {
		title = before
	}
	title = strings.ReplaceAll(title, "-", " ")
	return title
}
