package scanner

import (
	"path"
	"strings"
)

type ignoreMatcher struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	raw       string
	pattern   string
	directory bool
	anchored  bool
}

func newIgnoreMatcher(rules []IgnoreRule) ignoreMatcher {
	patterns := make([]ignorePattern, 0, len(rules))
	for _, rule := range rules {
		pattern := strings.TrimSpace(rule.Pattern)
		if pattern == "" || strings.HasPrefix(pattern, "!") {
			continue
		}
		directory := strings.HasSuffix(pattern, "/")
		pattern = strings.Trim(pattern, "/")
		if pattern == "" {
			continue
		}
		anchored := strings.HasPrefix(rule.Pattern, "/")
		patterns = append(patterns, ignorePattern{
			raw:       rule.Pattern,
			pattern:   path.Clean(strings.ReplaceAll(pattern, "\\", "/")),
			directory: directory,
			anchored:  anchored,
		})
	}
	return ignoreMatcher{patterns: patterns}
}

func (m ignoreMatcher) ignored(relativePath string, isDir bool) bool {
	relativePath = cleanRelativePath(relativePath)
	if relativePath == "." {
		return false
	}
	relativePath = path.Clean(strings.ReplaceAll(relativePath, "\\", "/"))
	base := path.Base(relativePath)
	for _, pattern := range m.patterns {
		if pattern.directory && !isDir {
			continue
		}
		if pattern.matches(relativePath, base) {
			return true
		}
	}
	return false
}

func (p ignorePattern) matches(relativePath, base string) bool {
	pattern := p.pattern
	if pattern == relativePath {
		return true
	}
	if !strings.Contains(pattern, "/") {
		if pattern == base {
			return true
		}
		for _, segment := range strings.Split(relativePath, "/") {
			if segment == pattern {
				return true
			}
		}
	}
	if strings.HasPrefix(pattern, "**/") {
		suffix := strings.TrimPrefix(pattern, "**/")
		return relativePath == suffix || strings.HasSuffix(relativePath, "/"+suffix)
	}
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return relativePath == prefix || strings.HasPrefix(relativePath, prefix+"/")
	}
	if matched, err := path.Match(pattern, relativePath); err == nil && matched {
		return true
	}
	if p.anchored {
		return false
	}
	return strings.HasSuffix(relativePath, "/"+pattern)
}
