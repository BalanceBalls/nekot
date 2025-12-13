package websearch

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

const (
	defaultK1 = 1.5
	defaultB  = 0.75
)

type SearchResult struct {
	DocID int
	Score float64
}

type BM25 struct {
	TF           []map[string]int
	DocLengths   []int
	DocCount     int
	AvgDocLength float64
	IDF          map[string]float64
	k1           float64
	b            float64
}

func NewBM25(content []PageChunk) *BM25 {
	bm := &BM25{
		TF:         make([]map[string]int, len(content)),
		DocLengths: make([]int, len(content)),
		DocCount:   len(content),
		IDF:        make(map[string]float64),
		k1:         defaultK1,
		b:          defaultB,
	}

	totalLength := 0
	docFreq := make(map[string]int)

	for i, doc := range content {
		tokens := tokenize(doc.Content)
		bm.DocLengths[i] = len(tokens)
		totalLength += len(tokens)

		tf := make(map[string]int)
		for _, token := range tokens {
			tf[token]++
		}
		bm.TF[i] = tf

		for token := range tf {
			docFreq[token]++
		}
	}

	if bm.DocCount > 0 {
		bm.AvgDocLength = float64(totalLength) / float64(bm.DocCount)
	}

	for term, freq := range docFreq {
		n := float64(freq)
		N := float64(bm.DocCount)
		idf := math.Log(1 + (N-n+0.5)/(n+0.5))
		bm.IDF[term] = idf
	}

	return bm
}

func (bm *BM25) Search(query string) []SearchResult {
	queryTokens := tokenize(query)
	results := make([]SearchResult, bm.DocCount)

	for i := 0; i < bm.DocCount; i++ {
		score := 0.0
		docLen := float64(bm.DocLengths[i])

		for _, token := range queryTokens {
			idf, ok := bm.IDF[token]
			if !ok {
				continue
			}

			tf := float64(bm.TF[i][token])

			numerator := idf * tf * (bm.k1 + 1)
			denominator := tf + bm.k1*(1-bm.b+bm.b*(docLen/bm.AvgDocLength))

			score += numerator / denominator
		}

		results[i] = SearchResult{DocID: i, Score: score}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

func tokenize(text string) []string {
	f := func(c rune) bool {
		return !unicode.IsLetter(c) && !unicode.IsNumber(c)
	}

	fields := strings.FieldsFunc(text, f)

	var tokens []string
	for _, field := range fields {
		tokens = append(tokens, strings.ToLower(field))
	}
	return tokens
}
