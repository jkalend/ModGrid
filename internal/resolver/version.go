package resolver

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type version [3]uint64

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
var termPattern = regexp.MustCompile(`^(>=|<=|=|>|<)?([0-9]+\.[0-9]+\.[0-9]+)$`)

func parseVersion(s string) (version, error) {
	var v version
	m := versionPattern.FindStringSubmatch(s)
	if m == nil {
		return v, failure("UnsupportedVersion", fmt.Sprintf("expected FixtureVersionV1 major.minor.patch, got %q", s))
	}
	for i := range v {
		n, err := strconv.ParseUint(m[i+1], 10, 32)
		if err != nil {
			return v, failure("UnsupportedVersion", "version component exceeds uint32")
		}
		v[i] = n
	}
	return v, nil
}

func compare(a, b version) int {
	for i := range a {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

type term struct {
	op string
	v  version
}

func parseConstraint(s string) ([]term, error) {
	parts := strings.Split(s, ",")
	if len(parts) > 8 {
		return nil, failure("ResourceLimit", "constraint exceeds 8 terms")
	}
	terms := make([]term, 0, len(parts))
	for _, p := range parts {
		m := termPattern.FindStringSubmatch(strings.Trim(p, " \t\r\n"))
		if m == nil || (m[1] == "" && len(parts) != 1) {
			return nil, failure("UnsupportedConstraint", fmt.Sprintf("unsupported FixtureVersionV1 constraint %q", s))
		}
		v, err := parseVersion(m[2])
		if err != nil {
			return nil, err
		}
		op := m[1]
		if op == "" {
			op = "="
		}
		terms = append(terms, term{op, v})
	}
	return terms, nil
}

func matches(v version, constraint string) bool {
	terms, _ := parseConstraint(constraint) // Validated before entering the search.
	for _, t := range terms {
		c := compare(v, t.v)
		if (t.op == "=" && c != 0) || (t.op == ">=" && c < 0) ||
			(t.op == "<=" && c > 0) || (t.op == ">" && c <= 0) || (t.op == "<" && c >= 0) {
			return false
		}
	}
	return true
}
