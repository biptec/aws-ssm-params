package client

// chunkStrings splits a slice into non-empty chunks. AWS SSM APIs have small
// per-request limits, so client methods use this helper to keep batching rules
// explicit at the call site.
func chunkStrings(values []string, size int) [][]string {
	if size <= 0 {
		size = 10
	}

	var chunks [][]string

	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}

		chunks = append(chunks, values[start:end])
	}

	return chunks
}
