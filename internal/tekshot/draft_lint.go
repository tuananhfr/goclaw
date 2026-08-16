package tekshot

import (
	"fmt"
	"strings"
	"unicode"
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
	// lintMinSegmentWords: shorter brief lines (labels, prices, product names)
	// are legitimately reused word for word and must not count as echoes.
	lintMinSegmentWords = 5
	// lintSegmentEchoRatio: share of a segment's word bigrams that must survive
	// into the content before that line counts as echoed. Calibrated on the
	// 2026-08-16 batch: real rewordings land at 0.5-0.6, developed prose at 0.1-0.3.
	lintSegmentEchoRatio = 0.45
	// lintBriefEchoShare / lintMinBriefSegments: flag only when half the brief
	// comes back, and only for briefs with enough lines for that to mean anything.
	lintBriefEchoShare   = 0.5
	lintMinBriefSegments = 3
	// lintLabelMaxWords: "Hook:", "Kết & CTA:" are labels; "Sợi mỳ Ý dai ngon:"
	// is content that happens to end in a colon.
	lintLabelMaxWords = 3
)

// lintWords lowercases and keeps only letter/digit runs, so punctuation and
// bullet glyphs cannot change a similarity score.
func lintWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

func wordBigrams(words []string) map[string]bool {
	out := make(map[string]bool, len(words))
	for i := 0; i+1 < len(words); i++ {
		out[words[i]+" "+words[i+1]] = true
	}
	return out
}

// briefSegments splits a brief into the units a human would judge separately:
// one per line, then per sentence, with leading field labels removed.
func briefSegments(brief string) []string {
	var out []string
	for _, line := range strings.FieldsFunc(brief, func(r rune) bool { return r == '\n' || r == '\r' }) {
		for _, sentence := range strings.FieldsFunc(line, func(r rune) bool {
			return r == '.' || r == '!' || r == '?' || r == ';'
		}) {
			segment := strings.TrimSpace(sentence)
			if idx := strings.Index(segment, ":"); idx >= 0 && len(lintWords(segment[:idx])) <= lintLabelMaxWords {
				segment = strings.TrimSpace(segment[idx+1:])
			}
			if len(lintWords(segment)) >= lintMinSegmentWords {
				out = append(out, segment)
			}
		}
	}
	return out
}

// briefEchoShare reports what fraction of the brief's judgeable lines reappear
// in the content as near-verbatim rewording, plus how many lines were judged.
func briefEchoShare(brief, content string) (float64, int) {
	segments := briefSegments(brief)
	if len(segments) == 0 {
		return 0, 0
	}
	contentBigrams := wordBigrams(lintWords(content))
	echoed := 0
	for _, segment := range segments {
		bigrams := wordBigrams(lintWords(segment))
		if len(bigrams) == 0 {
			continue
		}
		hits := 0
		for bigram := range bigrams {
			if contentBigrams[bigram] {
				hits++
			}
		}
		if float64(hits)/float64(len(bigrams)) >= lintSegmentEchoRatio {
			echoed++
		}
	}
	return float64(echoed) / float64(len(segments)), len(segments)
}

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

	// brief_copy above only catches a brief pasted whole. The far more common
	// failure is the model mirroring the brief line by line with synonyms, which
	// no substring test sees — hence the per-segment bigram check.
	rawBrief := item.SourceBrief
	if strings.TrimSpace(rawBrief) == "" {
		rawBrief = stringArg(post, "brief")
	}
	if share, segments := briefEchoShare(rawBrief, content); segments >= lintMinBriefSegments && share >= lintBriefEchoShare {
		findings = append(findings, LintFinding{
			Code:    "brief_paraphrase",
			Message: fmt.Sprintf("About %d%% of the brief's lines reappear in the content as near-verbatim rewording. The brief is raw material, not a draft: develop each point into finished prose that says something the brief does not.", int(share*100)),
		})
	}

	if utf8.RuneCountInString(normContent) < lintMinContentRunes {
		findings = append(findings, LintFinding{
			Code:    "content_too_short",
			Message: "The content is too short to develop the brief. Write a complete caption (hook, body, CTA).",
		})
	}

	return findings
}
