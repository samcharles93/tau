package indexing

import (
	"testing"
	"testing/fstest"
)

func TestIndexing(t *testing.T) {
	mockFS := fstest.MapFS{
		"config.md": &fstest.MapFile{
			Data: []byte("# Configuration\nThis is the configuration document. It contains settings like default_provider and default_model.\n"),
		},
		"skills.md": &fstest.MapFile{
			Data: []byte("# Skills\nThis document describes how to write custom skills for Tau.\n"),
		},
		"sessions.md": &fstest.MapFile{
			Data: []byte("# Sessions\nThis document describes session persistence and branching.\n"),
		},
		"some-doc-name.md": &fstest.MapFile{
			Data: []byte("# Some Doc Title\nContent here.\n"),
		},
		"ignored.go": &fstest.MapFile{
			Data: []byte("package main\n\nfunc main() {}\n"),
		},
	}

	idx, err := Build(mockFS)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if idx == nil {
		t.Fatalf("Build returned nil Index")
	}
	defer idx.Close()

	// 1. Standard build and search (single word "configuration")
	res, err := idx.Search("configuration", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(res.Hits) == 0 {
		t.Fatalf("Expected at least one match for 'configuration'")
	}
	if res.Hits[0].Path != "config.md" {
		t.Errorf("Expected first hit path to be 'config.md', got %s", res.Hits[0].Path)
	}
	if res.Hits[0].Title != "config" {
		t.Errorf("Expected first hit title to be 'config', got %s", res.Hits[0].Title)
	}
	if res.Hits[0].Score <= 0 {
		t.Errorf("Expected positive score, got %f", res.Hits[0].Score)
	}
	if len(res.Hits[0].Fragments) == 0 {
		t.Errorf("Expected non-empty fragments")
	}

	// 2. Multi-word query ("session config") returns matching documents (should match both config.md and sessions.md)
	resMulti, err := idx.Search("session config", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(resMulti.Hits) < 2 {
		t.Errorf("Expected at least 2 matches for 'session config', got %d", len(resMulti.Hits))
	}

	// 3. Searching for a non-existent word returns zero matches without error
	resNone, err := idx.Search("nonexistentwordxyz", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(resNone.Hits) != 0 {
		t.Errorf("Expected 0 matches for nonexistent word, got %d", len(resNone.Hits))
	}

	// 4. Empty search query returns zero matches without error
	resEmpty, err := idx.Search("", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(resEmpty.Hits) != 0 {
		t.Errorf("Expected 0 matches for empty query, got %d", len(resEmpty.Hits))
	}
}
