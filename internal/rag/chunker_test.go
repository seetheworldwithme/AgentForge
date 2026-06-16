package rag

import (
	"strings"
	"testing"
)

func TestChunkSplitsBySizeWithOverlap(t *testing.T) {
	text := strings.Repeat("a", 2000) // 2000 chars
	chunks := Chunk(text, 800, 100)
	if len(chunks) < 3 {
		t.Fatalf("len=%d, want >=3", len(chunks))
	}
	// each chunk should be at most size, non-empty
	for i, c := range chunks {
		if len(c) == 0 {
			t.Errorf("chunk %d empty", i)
		}
		if len(c) > 800 {
			t.Errorf("chunk %d len=%d > 800", i, len(c))
		}
	}
}

func TestChunkOverlapCarriesBetweenChunks(t *testing.T) {
	// step = size - overlap = 800 - 100 = 700; the last 100 chars of chunk 0
	// equal the first 100 chars of chunk 1.
	text := strings.Repeat("abcdefghij", 200) // 2000 chars, 10-char cycle
	chunks := Chunk(text, 800, 100)
	if len(chunks) < 2 {
		t.Fatalf("len=%d", len(chunks))
	}
	tail := chunks[0][len(chunks[0])-100:]
	head := chunks[1][:100]
	if tail != head {
		t.Errorf("overlap mismatch:\n tail=%q\n head=%q", tail, head)
	}
}

func TestChunkSmallTextReturnsOne(t *testing.T) {
	got := Chunk("hello", 800, 100)
	if len(got) != 1 || got[0] != "hello" {
		t.Errorf("got=%+v", got)
	}
}

func TestChunkEmptyReturnsNil(t *testing.T) {
	got := Chunk("", 800, 100)
	if got != nil {
		t.Errorf("got=%+v, want nil", got)
	}
}
