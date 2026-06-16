package rag

// Chunk splits text into pieces of at most size chars, overlapping by overlap
// chars between consecutive chunks.
func Chunk(text string, size, overlap int) []string {
	if size <= 0 {
		size = 800
	}
	if overlap < 0 || overlap >= size {
		overlap = size / 8
	}
	if len(text) == 0 {
		return nil
	}
	if len(text) <= size {
		return []string{text}
	}
	var out []string
	step := size - overlap
	for start := 0; start < len(text); start += step {
		end := start + size
		if end > len(text) {
			end = len(text)
		}
		out = append(out, text[start:end])
		if end == len(text) {
			break
		}
	}
	return out
}
