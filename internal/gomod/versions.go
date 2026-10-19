// Package gomod implements the Go module proxy path, version and archive rules
// used by kkrepo-go. It is a direct port of the Java implementation in
// kkRepo's protocol-go and server/goartifact packages.
package gomod

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	versionNumber = `0|[1-9][0-9]*`
	versionPreID  = `(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)`
)

var (
	versionRE = regexp.MustCompile(
		`^v(` + versionNumber + `)\.(` + versionNumber + `)\.(` + versionNumber + `)` +
			`(?:-(` + versionPreID + `(?:\.` + versionPreID + `)*))?` +
			`(?:\+(incompatible))?$`)
	// pseudoRE mirrors golang.org/x/mod/module.pseudoVersionRE. A tagged
	// prerelease that merely ends in a timestamp and revision is not a
	// pseudo-version.
	pseudoRE = regexp.MustCompile(
		`^v[0-9]+\.(?:0\.0-|[0-9]+\.[0-9]+-(?:[^+]*\.)?0\.)([0-9]{14})-([A-Za-z0-9]+)(?:\+incompatible)?$`)
)

// RequireCanonicalVersion validates a canonical Go module version.
func RequireCanonicalVersion(value string) (string, error) {
	if value == "" || !versionRE.MatchString(value) {
		return "", fmt.Errorf("invalid canonical Go module version: %s", value)
	}
	return value, nil
}

// IsCanonicalVersion reports whether value is a canonical Go module version.
func IsCanonicalVersion(value string) bool {
	_, err := RequireCanonicalVersion(value)
	return err == nil
}

// EscapeVersion applies the Go proxy case encoding to a version.
func EscapeVersion(version string) (string, error) {
	value, err := RequireCanonicalVersion(version)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.Grow(len(value))
	for _, ch := range value {
		if ch >= 'A' && ch <= 'Z' {
			out.WriteByte('!')
			out.WriteRune(ch - 'A' + 'a')
		} else {
			out.WriteRune(ch)
		}
	}
	return out.String(), nil
}

// UnescapeVersion reverses the Go proxy case encoding for a version.
func UnescapeVersion(escaped string) (string, error) {
	if strings.TrimSpace(escaped) == "" {
		return "", fmt.Errorf("invalid escaped Go module version: %s", escaped)
	}
	var decoded strings.Builder
	decoded.Grow(len(escaped))
	for i := 0; i < len(escaped); i++ {
		ch := escaped[i]
		if ch == '!' {
			i++
			if i >= len(escaped) {
				return "", fmt.Errorf("invalid escaped Go module version: %s", escaped)
			}
			lower := escaped[i]
			if lower < 'a' || lower > 'z' {
				return "", fmt.Errorf("invalid escaped Go module version: %s", escaped)
			}
			decoded.WriteByte(lower - 'a' + 'A')
			continue
		}
		if ch >= 'A' && ch <= 'Z' {
			return "", fmt.Errorf("invalid escaped Go module version: %s", escaped)
		}
		decoded.WriteByte(ch)
	}
	version, err := RequireCanonicalVersion(decoded.String())
	if err != nil {
		return "", fmt.Errorf("invalid escaped Go module version: %s", escaped)
	}
	canonical, _ := EscapeVersion(version)
	if canonical != escaped {
		return "", fmt.Errorf("non-canonical escaped Go module version: %s", escaped)
	}
	return version, nil
}

// Major returns the int major version of a canonical version.
func Major(value string) (int, error) {
	parsed, err := parseVersion(value)
	if err != nil {
		return 0, err
	}
	// The Java implementation stores the major in a 32-bit int.
	if !parsed.major.IsInt64() || parsed.major.Int64() > math.MaxInt32 {
		return 0, fmt.Errorf("Go module major version is too large: %s", value)
	}
	return int(parsed.major.Int64()), nil
}

// IsPrerelease reports whether a canonical version has a prerelease suffix.
func IsPrerelease(value string) bool {
	parsed, err := parseVersion(value)
	return err == nil && parsed.prerelease != ""
}

// IsPseudoVersion reports whether value is a canonical pseudo-version.
func IsPseudoVersion(value string) bool {
	if _, err := RequireCanonicalVersion(value); err != nil {
		return false
	}
	return pseudoRE.MatchString(value)
}

// PseudoTimestamp extracts the embedded UTC timestamp of a pseudo-version.
func PseudoTimestamp(value string) (time.Time, bool) {
	if _, err := RequireCanonicalVersion(value); err != nil {
		return time.Time{}, false
	}
	match := pseudoRE.FindStringSubmatch(value)
	if match == nil {
		return time.Time{}, false
	}
	stamp, err := time.Parse("20060102150405", match[1])
	if err != nil {
		return time.Time{}, false
	}
	return stamp.UTC(), true
}

// CompareVersions implements SemVer precedence for canonical Go versions.
// Build metadata (the +incompatible suffix) is ignored.
func CompareVersions(left, right string) int {
	a, err := parseVersion(left)
	if err != nil {
		return strings.Compare(left, right)
	}
	b, err := parseVersion(right)
	if err != nil {
		return strings.Compare(left, right)
	}
	if c := a.major.Cmp(b.major); c != 0 {
		return c
	}
	if c := a.minor.Cmp(b.minor); c != 0 {
		return c
	}
	if c := a.patch.Cmp(b.patch); c != 0 {
		return c
	}
	return comparePrerelease(a.prerelease, b.prerelease)
}

func comparePrerelease(left, right string) int {
	if left == "" && right == "" {
		return 0
	}
	if left == "" {
		return 1
	}
	if right == "" {
		return -1
	}
	a := strings.Split(left, ".")
	b := strings.Split(right, ".")
	max := len(a)
	if len(b) > max {
		max = len(b)
	}
	for i := 0; i < max; i++ {
		if i >= len(a) {
			return -1
		}
		if i >= len(b) {
			return 1
		}
		if c := compareIdentifier(a[i], b[i]); c != 0 {
			return c
		}
	}
	return 0
}

func compareIdentifier(left, right string) int {
	leftNumeric := isAllDigits(left)
	rightNumeric := isAllDigits(right)
	if leftNumeric && rightNumeric {
		a, _ := new(big.Int).SetString(left, 10)
		b, _ := new(big.Int).SetString(right, 10)
		return a.Cmp(b)
	}
	if leftNumeric {
		return -1
	}
	if rightNumeric {
		return 1
	}
	return strings.Compare(left, right)
}

func isAllDigits(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

// ListVersions filters out pseudo-versions, removes duplicates and sorts the
// remaining versions in ascending SemVer order.
func ListVersions(versions []string) []string {
	seen := make(map[string]struct{}, len(versions))
	out := make([]string, 0, len(versions))
	for _, version := range versions {
		canonical, err := RequireCanonicalVersion(version)
		if err != nil || IsPseudoVersion(canonical) {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	sort.Slice(out, func(i, j int) bool {
		if c := CompareVersions(out[i], out[j]); c != 0 {
			return c < 0
		}
		return out[i] < out[j]
	})
	return out
}

// Candidate is a version/time pair used for @latest selection.
type Candidate struct {
	Version string
	Time    time.Time
}

// Latest selects releases before prereleases, and pseudo-versions only when no
// tagged version exists. It mirrors GoVersions.latest in the Java server.
func Latest(candidates []Candidate) (Candidate, bool) {
	valid := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if _, err := RequireCanonicalVersion(candidate.Version); err == nil {
			valid = append(valid, candidate)
		}
	}
	if len(valid) == 0 {
		return Candidate{}, false
	}
	var release *Candidate
	for i := range valid {
		if !IsPrerelease(valid[i].Version) && (release == nil || compareCandidates(valid[i], *release) > 0) {
			release = &valid[i]
		}
	}
	if release != nil {
		return *release, true
	}
	var tagged *Candidate
	for i := range valid {
		if !IsPseudoVersion(valid[i].Version) && (tagged == nil || compareCandidates(valid[i], *tagged) > 0) {
			tagged = &valid[i]
		}
	}
	if tagged != nil {
		return *tagged, true
	}
	best := valid[0]
	for _, candidate := range valid[1:] {
		if compareCandidateTime(candidate, best) > 0 {
			best = candidate
		}
	}
	return best, true
}

func compareCandidates(a, b Candidate) int {
	if c := CompareVersions(a.Version, b.Version); c != 0 {
		return c
	}
	return strings.Compare(a.Version, b.Version)
}

func compareCandidateTime(a, b Candidate) int {
	at := candidateTime(a)
	bt := candidateTime(b)
	if at.Before(bt) {
		return -1
	}
	if at.After(bt) {
		return 1
	}
	return compareCandidates(a, b)
}

func candidateTime(candidate Candidate) time.Time {
	if stamp, ok := PseudoTimestamp(candidate.Version); ok {
		return stamp
	}
	if candidate.Time.IsZero() {
		return time.Unix(0, 0).UTC()
	}
	return candidate.Time
}

type parsedVersion struct {
	major      *big.Int
	minor      *big.Int
	patch      *big.Int
	prerelease string
}

func parseVersion(value string) (parsedVersion, error) {
	canonical, err := RequireCanonicalVersion(value)
	if err != nil {
		return parsedVersion{}, err
	}
	match := versionRE.FindStringSubmatch(canonical)
	if match == nil {
		return parsedVersion{}, fmt.Errorf("invalid Go module version: %s", value)
	}
	major, _ := new(big.Int).SetString(match[1], 10)
	minor, _ := new(big.Int).SetString(match[2], 10)
	patch, _ := new(big.Int).SetString(match[3], 10)
	return parsedVersion{
		major:      major,
		minor:      minor,
		patch:      patch,
		prerelease: match[4],
	}, nil
}
