package byteslice

import "bytes"

func IndexAll(p, sub []byte) []int {
	if len(sub) == 0 || len(p) < len(sub) {
		return nil
	}

	var indexes []int

	for i := 0; i <= len(p)-len(sub); {
		idx := bytes.Index(p[i:], sub)
		if idx == -1 {
			break
		}
		matchStart := idx + i
		indexes = append(indexes, matchStart)
		i = matchStart + len(sub) // Start next search after this match
	}

	return indexes
}

// IndexALlPartial is a helper function that returns results of both IndexAll and PartialIndex in one call.
func IndexAllPartial(p, sub []byte) ([]int, int) {
	indexes := IndexAll(p, sub)
	partial := PartialIndex(p, sub)
	if partial != -1 && len(indexes) > 0 && indexes[len(indexes)-1] == partial {
		partial = -1
	}
	return indexes, partial
}
