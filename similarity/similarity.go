package similarity

import (
	"errors"
	"hash/fnv"
	"math"
	"regexp"
	"strings"

	"github.com/minicago/gooj/sql_service"
)

// errTooManySubmissions is returned when a problem has too many submissions to
// run the O(n^2) pairwise comparison.
var errTooManySubmissions = errors.New("too many submissions to compare (limit exceeded)")

// DefaultK is the shingle (k-gram) size used for token-based similarity.
const DefaultK = 5

// DefaultThreshold is the minimum Jaccard similarity above which a pair is stored.
const DefaultThreshold = 0.6

var (
	blockCommentRE = regexp.MustCompile(`(?s)/\*.*?\*/`)
	lineCommentRE  = regexp.MustCompile(`//[^\n]*`)
	stringLitRE    = regexp.MustCompile(`"(?:\\.|[^"\\])*"`)
	tokenRE        = regexp.MustCompile(`[A-Za-z0-9_]+`)
)

// normalizeCode strips comments, string literals and normalizes whitespace so
// superficial differences (formatting, variable names kept as tokens) are reduced.
// Note: it does NOT rename identifiers, so renamed variables still reduce
// similarity — which is the desired behaviour for a lightweight check.
func normalizeCode(code string) string {
	code = blockCommentRE.ReplaceAllString(code, " ")
	code = lineCommentRE.ReplaceAllString(code, " ")
	code = stringLitRE.ReplaceAllString(code, `""`)
	code = strings.ToLower(code)
	return code
}

// tokenize splits normalized code into identifier/number tokens.
func tokenize(code string) []string {
	return tokenRE.FindAllString(normalizeCode(code), -1)
}

// shingles returns the set of k-gram (consecutive token sequence) hashes.
func shingles(tokens []string, k int) map[uint64]struct{} {
	set := make(map[uint64]struct{})
	if len(tokens) < k {
		// fall back to a single shingle of all tokens so tiny submissions still compare
		h := fnvHash(strings.Join(tokens, " "))
		set[h] = struct{}{}
		return set
	}
	for i := 0; i <= len(tokens)-k; i++ {
		h := fnvHash(strings.Join(tokens[i:i+k], " "))
		set[h] = struct{}{}
	}
	return set
}

// fnvHash computes an FNV-1a 64-bit hash of a string.
func fnvHash(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// jaccard computes the Jaccard similarity of two shingle sets in [0,1].
func jaccard(a, b map[uint64]struct{}) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	intersection := 0
	for k := range a {
		if _, ok := b[k]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// Similarity returns the Jaccard similarity (in [0,1]) between two code snippets
// using token k-grams.
func Similarity(a, b string, k int) float64 {
	if k <= 0 {
		k = DefaultK
	}
	sa := shingles(tokenize(a), k)
	sb := shingles(tokenize(b), k)
	return jaccard(sa, sb)
}

// CheckProblem computes pairwise code similarity for all submissions of a problem,
// stores pairs whose similarity is at least `threshold` (defaulting to
// DefaultThreshold), and returns the stored records.
func CheckProblem(problemID uint, threshold float64) ([]sql_service.SimilarityRecord, error) {
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	subs, err := sql_service.GetSubmissionsByProblem(problemID)
	if err != nil {
		return nil, err
	}

	records := make([]sql_service.SimilarityRecord, 0)
	maxPairs := 0
	for i := 0; i < len(subs); i++ {
		maxPairs += len(subs) - 1 - i
	}
	// Guard against pathological O(n^2) with huge submission counts.
	const hardLimit = 200000
	if maxPairs > hardLimit {
		return nil, errTooManySubmissions
	}

	for i := 0; i < len(subs); i++ {
		for j := i + 1; j < len(subs); j++ {
			sim := Similarity(subs[i].Code, subs[j].Code, DefaultK)
			if sim >= threshold {
				records = append(records, sql_service.SimilarityRecord{
					ProblemID:   problemID,
					SubmissionA: subs[i].ID,
					SubmissionB: subs[j].ID,
					UserA:       subs[i].Username,
					UserB:       subs[j].Username,
					Similarity:  math.Round(sim*1000) / 1000,
				})
			}
		}
	}

	if err := sql_service.DeleteSimilarityByProblem(problemID); err != nil {
		return nil, err
	}
	if len(records) > 0 {
		if err := sql_service.SaveSimilarityRecords(records); err != nil {
			return nil, err
		}
	}
	return records, nil
}
