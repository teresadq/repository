package gomod

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const maxModulePathLength = 1024

const allowedModulePathChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~/"

var (
	windowsReserved = map[string]struct{}{
		"con": {}, "prn": {}, "aux": {}, "nul": {},
		"com1": {}, "com2": {}, "com3": {}, "com4": {}, "com5": {}, "com6": {}, "com7": {}, "com8": {}, "com9": {},
		"lpt1": {}, "lpt2": {}, "lpt3": {}, "lpt4": {}, "lpt5": {}, "lpt6": {}, "lpt7": {}, "lpt8": {}, "lpt9": {},
	}
	windowsShortName = regexp.MustCompile(`.*~[0-9]+$`)
)

// RequireModulePath validates a Go module path.
func RequireModulePath(value string) (string, error) {
	if value == "" || strings.TrimSpace(value) == "" || len(value) > maxModulePathLength ||
		strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") || strings.Contains(value, "//") {
		return "", invalidModulePath(value)
	}
	for i := 0; i < len(value); i++ {
		if strings.IndexByte(allowedModulePathChars, value[i]) < 0 {
			return "", invalidModulePath(value)
		}
	}
	for _, segment := range strings.Split(value, "/") {
		if err := requireModuleSegment(segment, value); err != nil {
			return "", err
		}
	}
	if err := requireFirstSegment(value); err != nil {
		return "", err
	}
	if err := requirePathMajorShape(value); err != nil {
		return "", err
	}
	return value, nil
}

// EscapeModulePath applies the Go proxy case encoding to a module path.
func EscapeModulePath(module string) (string, error) {
	value, err := RequireModulePath(module)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	out.Grow(len(value))
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if ch >= 'A' && ch <= 'Z' {
			out.WriteByte('!')
			out.WriteByte(ch - 'A' + 'a')
		} else {
			out.WriteByte(ch)
		}
	}
	return out.String(), nil
}

// UnescapeModulePath reverses the Go proxy case encoding for a module path.
func UnescapeModulePath(escaped string) (string, error) {
	if strings.TrimSpace(escaped) == "" {
		return "", fmt.Errorf("invalid escaped Go module path: %s", escaped)
	}
	var decoded strings.Builder
	decoded.Grow(len(escaped))
	for i := 0; i < len(escaped); i++ {
		ch := escaped[i]
		if ch == '!' {
			i++
			if i >= len(escaped) {
				return "", fmt.Errorf("invalid escaped Go module path: %s", escaped)
			}
			lower := escaped[i]
			if lower < 'a' || lower > 'z' {
				return "", fmt.Errorf("invalid escaped Go module path: %s", escaped)
			}
			decoded.WriteByte(lower - 'a' + 'A')
			continue
		}
		if ch >= 'A' && ch <= 'Z' {
			return "", fmt.Errorf("invalid escaped Go module path: %s", escaped)
		}
		decoded.WriteByte(ch)
	}
	module, err := RequireModulePath(decoded.String())
	if err != nil {
		return "", fmt.Errorf("invalid escaped Go module path: %s", escaped)
	}
	canonical, _ := EscapeModulePath(module)
	if canonical != escaped {
		return "", fmt.Errorf("non-canonical escaped Go module path: %s", escaped)
	}
	return module, nil
}

// RequireVersionSuffix enforces the /vN path suffix rules from the Go module
// specification.
func RequireVersionSuffix(module, version string) error {
	path, err := RequireModulePath(module)
	if err != nil {
		return err
	}
	canonical, err := RequireCanonicalVersion(version)
	if err != nil {
		return err
	}
	major, err := Major(canonical)
	if err != nil {
		return err
	}
	suffix := suffixMajor(path)
	if strings.HasPrefix(path, "gopkg.in/") {
		legacyV1Pseudo := suffix != nil && *suffix == 1 && major == 0 &&
			strings.HasPrefix(canonical, "v0.0.0-") && IsPseudoVersion(canonical)
		if suffix == nil || (*suffix != major && !legacyV1Pseudo) {
			return fmt.Errorf("Go module path major suffix does not match version: %s %s", path, version)
		}
		return nil
	}
	if major >= 2 && !strings.HasSuffix(canonical, "+incompatible") {
		if suffix == nil || *suffix != major {
			return fmt.Errorf("Go module path %s must end in /v%d for version %s", path, major, version)
		}
	} else if suffix != nil && *suffix >= 2 && *suffix != major {
		return fmt.Errorf("Go module path major suffix does not match version: %s %s", path, version)
	}
	return nil
}

func suffixMajor(module string) *int {
	leaf := module
	if slash := strings.LastIndexByte(module, '/'); slash >= 0 {
		leaf = module[slash+1:]
	}
	if strings.HasPrefix(module, "gopkg.in/") && strings.HasSuffix(leaf, "-unstable") {
		leaf = strings.TrimSuffix(leaf, "-unstable")
	}
	candidate := ""
	if strings.HasPrefix(leaf, "v") {
		candidate = leaf[1:]
	} else if strings.HasPrefix(module, "gopkg.in/") && strings.Contains(leaf, ".v") {
		candidate = leaf[strings.LastIndex(leaf, ".v")+2:]
	}
	if candidate == "" || !isAllDigits(candidate) {
		return nil
	}
	major, err := strconv.Atoi(candidate)
	if err != nil {
		return nil
	}
	return &major
}

func requireModuleSegment(segment, fullPath string) error {
	if segment == "" || onlyDots(segment) || strings.HasPrefix(segment, ".") || strings.HasSuffix(segment, ".") {
		return invalidModulePath(fullPath)
	}
	base := segment
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	if _, reserved := windowsReserved[strings.ToLower(base)]; reserved {
		return invalidModulePath(fullPath)
	}
	if windowsShortName.MatchString(base) {
		return invalidModulePath(fullPath)
	}
	return nil
}

func requireFirstSegment(value string) error {
	first := value
	if slash := strings.IndexByte(value, '/'); slash >= 0 {
		first = value[:slash]
	}
	if !strings.Contains(first, ".") || strings.HasPrefix(first, "-") {
		return invalidModulePath(value)
	}
	for i := 0; i < len(first); i++ {
		ch := first[i]
		if !(ch == '-' || ch == '.' || ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'z') {
			return invalidModulePath(value)
		}
	}
	return nil
}

func requirePathMajorShape(value string) error {
	leaf := value
	if slash := strings.LastIndexByte(value, '/'); slash >= 0 {
		leaf = value[slash+1:]
	}
	if strings.HasPrefix(value, "gopkg.in/") {
		stableLeaf := strings.TrimSuffix(leaf, "-unstable")
		marker := strings.LastIndex(stableLeaf, ".v")
		if marker < 1 || marker+2 >= len(stableLeaf) {
			return invalidModulePath(value)
		}
		major := stableLeaf[marker+2:]
		if !isAllDigits(major) || (len(major) > 1 && major[0] == '0') {
			return invalidModulePath(value)
		}
		return nil
	}
	if !strings.HasPrefix(leaf, "v") || len(leaf) == 1 {
		return nil
	}
	candidate := leaf[1:]
	for i := 0; i < len(candidate); i++ {
		ch := candidate[i]
		if !(ch >= '0' && ch <= '9' || ch == '.') {
			return nil
		}
	}
	if !isAllDigits(candidate) || candidate[0] == '0' || candidate == "1" {
		return invalidModulePath(value)
	}
	return nil
}

func onlyDots(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] != '.' {
			return false
		}
	}
	return true
}

func invalidModulePath(value string) error {
	return fmt.Errorf("invalid Go module path: %s", value)
}
