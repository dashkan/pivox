package aichat

// estimateTokens returns a rough token count for the given text using
// a len/4 heuristic. This is an approximation for ASCII-English text;
// the 25% safety buffer in the model context budget absorbs drift.
func estimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	return (len(text) + 3) / 4
}
