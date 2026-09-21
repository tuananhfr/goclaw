package tekshot

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
// Đây là bản MẶC ĐỊNH trong code; bản admin sửa nằm ở Drupal và đi theo từng request.
func BuildGovernanceCatalog() GovernanceCatalog {
	rules := defaultGovernanceRules()
	checks := make([]GovernanceComplianceCheck, 0, len(rules.ComplianceChecks))
	for _, check := range rules.ComplianceChecks {
		profiles := append([]string{}, check.Profiles...)
		checks = append(checks, GovernanceComplianceCheck{Code: check.Code, Profiles: profiles, Rule: check.Rule})
	}
	return GovernanceCatalog{
		Version:            governanceVersion,
		Preamble:           rules.Preamble,
		AbsoluteRules:      rules.AbsoluteRules,
		RiskLevels:         rules.RiskLevels,
		RiskReasonRule:     rules.RiskReasonRule,
		ExceptionRule:      rules.ExceptionRule,
		ComplianceChecks:   checks,
		ComplianceWarnings: rules.ComplianceWarnings,
		ImageRules:         rules.ImageRules,
		MediaBranchRules:   rules.MediaBranchRules,
	}
}
