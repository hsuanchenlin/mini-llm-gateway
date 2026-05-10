package rag

// Chunk splits text into overlapping rune windows.
//
// `size` is the target chunk size in runes; `overlap` is how many runes each
// chunk shares with its predecessor (so retrieval doesn't miss content that
// straddles a boundary). Returns nil for empty input. If overlap >= size,
// it's clamped to size-1 so the loop always advances.
func Chunk(text string, size, overlap int) []string {
	if size <= 0 || text == "" {
		return nil
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= size {
		overlap = size - 1
	}
	runes := []rune(text)
	if len(runes) == 0 {
		return nil
	}
	if len(runes) <= size {
		return []string{string(runes)}
	}
	step := size - overlap
	var chunks []string
	for i := 0; i < len(runes); i += step {
		end := i + size
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
		if end == len(runes) {
			break
		}
	}
	return chunks
}
