package tekshot

import "strings"

// GovernanceComplianceCheck là một mục C của Prompt D. Profiles rỗng = áp cho mọi profile.
type GovernanceComplianceCheck struct {
	Code     string   `json:"code"`
	Profiles []string `json:"profiles"`
	Rule     string   `json:"rule"`
}

// GovernanceCatalog là toàn bộ phần Kim chỉ nam nằm trong GoClaw, đúng câu chữ model nhận.
type GovernanceCatalog struct {
	Version            string                      `json:"version"`
	Preamble           string                      `json:"preamble"`
	AbsoluteRules      []governanceRule            `json:"absolute_rules"`
	RiskLevels         []governanceRule            `json:"risk_levels"`
	RiskReasonRule     string                      `json:"risk_reason_rule"`
	ExceptionRule      string                      `json:"exception_rule"`
	ComplianceChecks   []GovernanceComplianceCheck `json:"compliance_checks"`
	ComplianceWarnings []governanceRule            `json:"compliance_warnings"`
	ImageRules         string                      `json:"image_rules"`
	MediaBranchRules   map[string]string           `json:"media_branch_rules"`
}

// BuildGovernanceCatalog không lọc theo profile: phía Drupal quyết định ai thấy phần nào.
func BuildGovernanceCatalog() GovernanceCatalog {
	checks := make([]GovernanceComplianceCheck, 0, len(complianceChecks))
	for _, check := range complianceChecks {
		profiles := append([]string{}, check.Profiles...)
		checks = append(checks, GovernanceComplianceCheck{Code: check.Code, Profiles: profiles, Rule: check.Rule})
	}
	warnings := make([]governanceRule, 0, len(complianceWarnings))
	for _, warning := range complianceWarnings {
		warnings = append(warnings, governanceRule{Code: warning.Code, Text: warning.Rule})
	}
	branches := map[string]string{}
	for _, branch := range []string{"UPLOAD", "REF", "INFO"} {
		branches[branch] = strings.TrimSpace(mediaBranchRules(branch))
	}
	return GovernanceCatalog{
		Version:            governanceVersion,
		Preamble:           governancePreamble,
		AbsoluteRules:      append([]governanceRule{}, absoluteRules...),
		RiskLevels:         append([]governanceRule{}, riskLevels...),
		RiskReasonRule:     riskReasonRule,
		ExceptionRule:      exceptionRule,
		ComplianceChecks:   checks,
		ComplianceWarnings: warnings,
		ImageRules:         strings.TrimSpace(imageRulesBlock),
		MediaBranchRules:   branches,
	}
}
