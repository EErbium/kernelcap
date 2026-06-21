package detector

import (
	"hash/fnv"
	"math/bits"
	"strings"
)

const simhashBitSize = 64

func SimHash(text string) uint64 {
	tokens := strings.Fields(text)
	if len(tokens) == 0 {
		return 0
	}

	var votes [simhashBitSize]int64

	for _, token := range tokens {
		h := hashToken(token)
		for i := 0; i < simhashBitSize; i++ {
			if h&(1<<uint(i)) != 0 {
				votes[i]++
			} else {
				votes[i]--
			}
		}
	}

	var fp uint64
	for i := 0; i < simhashBitSize; i++ {
		if votes[i] > 0 {
			fp |= 1 << uint(i)
		}
	}
	return fp
}

func hashToken(token string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(token))
	return h.Sum64()
}

func HammingDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

func NormalizedSimilarity(a, b uint64) float64 {
	d := HammingDistance(a, b)
	return 1.0 - float64(d)/float64(simhashBitSize)
}
