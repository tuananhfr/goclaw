package tekshot

import (
	"log/slog"
	"regexp"
	"strings"
	"unicode/utf8"
)

// governanceRules là bộ luật hệ thống một lượt viết/soát/sinh ảnh dùng.
//
// Drupal gửi bản admin sửa trong page_profile.system_rules, chỉ những khối đã
// sửa. Khối thiếu hoặc hỏng dùng mặc định trong code: luật không bao giờ rỗng.
type governanceRules struct {
	Preamble           string
	AbsoluteRules      []governanceRule
	RiskLevels         []governanceRule
	RiskReasonRule     string
	ExceptionRule      string
	ComplianceChecks   []complianceCheck
	ComplianceWarnings []governanceRule
	ImageRules         string
	MediaBranchRules   map[string]string
}

const (
	maxRuleItems     = 60
	maxRuleTextRunes = 2000
	maxBlockRunes    = 12000
)

var (
	ruleCodePattern    = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,12}$`)
	profileCodePattern = regexp.MustCompile(`^P[1-8]$`)
)

func defaultGovernanceRules() governanceRules {
	warnings := make([]governanceRule, 0, len(complianceWarnings))
	for _, warning := range complianceWarnings {
		warnings = append(warnings, governanceRule{Code: warning.Code, Text: warning.Rule})
	}
	branches := make(map[string]string, len(defaultMediaBranchRules))
	for key, text := range defaultMediaBranchRules {
		branches[key] = text
	}
	return governanceRules{
		Preamble:           governancePreamble,
		AbsoluteRules:      append([]governanceRule{}, absoluteRules...),
		RiskLevels:         append([]governanceRule{}, riskLevels...),
		RiskReasonRule:     riskReasonRule,
		ExceptionRule:      exceptionRule,
		ComplianceChecks:   append([]complianceCheck{}, complianceChecks...),
		ComplianceWarnings: warnings,
		ImageRules:         strings.TrimSpace(imageRulesBlock),
		MediaBranchRules:   branches,
	}
}

func resolveGovernanceRules(raw any) governanceRules {
	rules := defaultGovernanceRules()
	overrides, ok := raw.(map[string]any)
	if !ok || len(overrides) == 0 {
		return rules
	}

	applyText(overrides, "preamble", &rules.Preamble)
	applyText(overrides, "risk_reason_rule", &rules.RiskReasonRule)
	applyText(overrides, "exception_rule", &rules.ExceptionRule)
	applyText(overrides, "image_rules", &rules.ImageRules)

	if value, present := overrides["absolute_rules"]; present {
		if list, valid := parseRuleList(value); valid {
			rules.AbsoluteRules = list
		} else {
			warnRejected("absolute_rules")
		}
	}
	if value, present := overrides["compliance_warnings"]; present {
		if list, valid := parseRuleList(value); valid {
			rules.ComplianceWarnings = list
		} else {
			warnRejected("compliance_warnings")
		}
	}
	if value, present := overrides["risk_levels"]; present {
		if list, valid := parseRiskLevels(value); valid {
			rules.RiskLevels = list
		} else {
			warnRejected("risk_levels")
		}
	}
	if value, present := overrides["compliance_checks"]; present {
		if list, valid := parseComplianceChecks(value); valid {
			rules.ComplianceChecks = list
		} else {
			warnRejected("compliance_checks")
		}
	}
	if value, present := overrides["media_branch_rules"]; present {
		if branches, valid := value.(map[string]any); valid {
			for key := range rules.MediaBranchRules {
				if text, isText := cleanText(branches[key], maxRuleTextRunes); isText {
					rules.MediaBranchRules[key] = text
				}
			}
		} else {
			warnRejected("media_branch_rules")
		}
	}
	return rules
}

func applyText(overrides map[string]any, key string, target *string) {
	value, present := overrides[key]
	if !present {
		return
	}
	if text, valid := cleanText(value, maxBlockRunes); valid {
		*target = text
		return
	}
	warnRejected(key)
}

func warnRejected(block string) {
	slog.Warn("tekshot: governance override rejected, using default", "block", block)
}

func cleanText(value any, limit int) (string, bool) {
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > limit {
		return "", false
	}
	return text, true
}

// parseRuleList bỏ cả khối khi có một dòng hỏng: ghép nửa bản sửa với nửa mặc định sẽ ra bộ luật không ai viết.
func parseRuleList(value any) ([]governanceRule, bool) {
	items, ok := value.([]any)
	if !ok || len(items) == 0 || len(items) > maxRuleItems {
		return nil, false
	}
	seen := make(map[string]bool, len(items))
	out := make([]governanceRule, 0, len(items))
	for _, item := range items {
		row, isMap := item.(map[string]any)
		if !isMap {
			return nil, false
		}
		code := strings.TrimSpace(stringFromMap(row, "code"))
		text, validText := cleanText(row["text"], maxRuleTextRunes)
		if !ruleCodePattern.MatchString(code) || !validText || seen[code] {
			return nil, false
		}
		seen[code] = true
		out = append(out, governanceRule{Code: code, Text: text})
	}
	return out, true
}

// parseRiskLevels giữ đúng ba mã LOW/MEDIUM/HIGH: normalizeRisk và cổng duyệt dựa vào chúng.
func parseRiskLevels(value any) ([]governanceRule, bool) {
	list, ok := parseRuleList(value)
	if !ok || len(list) != 3 {
		return nil, false
	}
	byCode := make(map[string]string, 3)
	for _, level := range list {
		byCode[strings.ToUpper(level.Code)] = level.Text
	}
	out := make([]governanceRule, 0, 3)
	for _, code := range []string{riskLow, riskMedium, riskHigh} {
		text, present := byCode[code]
		if !present {
			return nil, false
		}
		out = append(out, governanceRule{Code: code, Text: text})
	}
	return out, true
}

func parseComplianceChecks(value any) ([]complianceCheck, bool) {
	items, ok := value.([]any)
	if !ok || len(items) == 0 || len(items) > maxRuleItems {
		return nil, false
	}
	seen := make(map[string]bool, len(items))
	out := make([]complianceCheck, 0, len(items))
	for _, item := range items {
		row, isMap := item.(map[string]any)
		if !isMap {
			return nil, false
		}
		code := strings.TrimSpace(stringFromMap(row, "code"))
		rule, validRule := cleanText(row["rule"], maxRuleTextRunes)
		if !ruleCodePattern.MatchString(code) || !validRule || seen[code] {
			return nil, false
		}
		var profiles []string
		for _, profile := range stringSliceFromAny(row["profiles"]) {
			profile = strings.ToUpper(profile)
			if !profileCodePattern.MatchString(profile) {
				return nil, false
			}
			profiles = append(profiles, profile)
		}
		seen[code] = true
		out = append(out, complianceCheck{Code: code, Profiles: profiles, Rule: rule})
	}
	return out, true
}
