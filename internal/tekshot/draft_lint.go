package tekshot

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// LintFinding is one deterministic defect found in a generated draft post.
// These checks catch mechanical failures (echoed title, copied brief, broken
// hashtags) without spending an LLM call — the review call only judges what
// string comparison cannot.
type LintFinding struct {
	Code    string
	Message string
}

const (
	// lintMinTitleRunes guards title_echo against false positives on very
	// short titles that legitimately reappear as words in the caption.
	lintMinTitleRunes = 8
	// lintMinBriefRunes: shorter briefs are legitimately absorbed verbatim.
	lintMinBriefRunes = 40
	// lintMinContentRunes: below this the caption cannot have developed the
	// brief; threshold intentionally conservative (a 2-line teaser is ~150).
	lintMinContentRunes = 120
)

func normalizeForLint(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// LintDraftPost runs every deterministic check against a single generated
// post. Findings feed a revise call as critique; codes are stable API for
// the review metadata Drupal stores.
func LintDraftPost(post map[string]any, item SourceItem) []LintFinding {
	var findings []LintFinding
	content := stringArg(post, "content")
	normContent := normalizeForLint(content)

	title := normalizeForLint(item.SourceTitle)
	if title == "" {
		title = normalizeForLint(stringArg(post, "title"))
	}
	if utf8.RuneCountInString(title) >= lintMinTitleRunes {
		firstLine := content
		if idx := strings.IndexAny(content, "\r\n"); idx >= 0 {
			firstLine = content[:idx]
		}
		if strings.Contains(normalizeForLint(firstLine), title) {
			findings = append(findings, LintFinding{
				Code:    "title_echo",
				Message: "The first line of content repeats the title verbatim. Open with a hook instead; the title is a returned field, not the caption opening.",
			})
		}
	}

	brief := normalizeForLint(item.SourceBrief)
	if brief == "" {
		brief = normalizeForLint(stringArg(post, "brief"))
	}
	if utf8.RuneCountInString(brief) >= lintMinBriefRunes && strings.Contains(normContent, brief) {
		findings = append(findings, LintFinding{
			Code:    "brief_copy",
			Message: "The content contains the brief copied verbatim. Develop every brief point into finished prose instead of pasting the brief.",
		})
	}

	if tags := strings.Fields(stringArg(post, "hashtags")); len(tags) > 0 {
		bad := false
		for _, tag := range tags {
			if !strings.HasPrefix(tag, "#") {
				bad = true
				break
			}
		}
		if bad || len(tags) < 3 || len(tags) > 5 {
			findings = append(findings, LintFinding{
				Code:    "hashtag_format",
				Message: fmt.Sprintf("hashtags must be 3-5 space-separated tokens each starting with #; got %d token(s): %q", len(tags), stringArg(post, "hashtags")),
			})
		}
	}

	if utf8.RuneCountInString(normContent) < lintMinContentRunes {
		findings = append(findings, LintFinding{
			Code:    "content_too_short",
			Message: "The content is too short to develop the brief. Write a complete caption (hook, body, CTA).",
		})
	}

	return findings
}
