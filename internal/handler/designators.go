package handler

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// rangeRe matches designators like "R1-R5" or "C10-C20" where prefix matches.
var rangeRe = regexp.MustCompile(`^([A-Za-z_]+)(\d+)-[A-Za-z_]*(\d+)$`)

// ExpandDesignators parses a reference designator string (e.g. "R1, R3, R5-R8, C1")
// and returns the individual designators expanded from any ranges.
func ExpandDesignators(s string) []string {
	var result []string
	for _, part := range strings.Split(s, ",") {
		tok := strings.TrimSpace(part)
		if tok == "" {
			continue
		}
		if m := rangeRe.FindStringSubmatch(tok); m != nil {
			prefix := m[1]
			lo, _ := strconv.Atoi(m[2])
			hi, _ := strconv.Atoi(m[3])
			if lo > hi {
				lo, hi = hi, lo
			}
			for n := lo; n <= hi; n++ {
				result = append(result, fmt.Sprintf("%s%d", prefix, n))
			}
		} else {
			result = append(result, tok)
		}
	}
	return result
}

// effectiveDesignatorCount returns how many designators in refDes are NOT in excluded.
// refDes is the raw reference_designators string from a BOM line item.
func effectiveDesignatorCount(refDes string, excluded map[string]struct{}) int {
	all := ExpandDesignators(refDes)
	count := 0
	for _, d := range all {
		if _, ok := excluded[d]; !ok {
			count++
		}
	}
	return count
}

// totalDesignatorCount returns the total number of individual designators in refDes.
func totalDesignatorCount(refDes string) int {
	return len(ExpandDesignators(refDes))
}

// buildExcludedSet converts a []string slice to a set for O(1) lookup.
func buildExcludedSet(designators []string) map[string]struct{} {
	m := make(map[string]struct{}, len(designators))
	for _, d := range designators {
		m[d] = struct{}{}
	}
	return m
}
