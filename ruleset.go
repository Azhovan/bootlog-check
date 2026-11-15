package bootlog

import "regexp"

// RuleSet owns immutable compiled rules. Its zero value is invalid; construct
// one with CompileRules. A RuleSet is safe for concurrent Evaluate calls.
// No field or accessor exposes mutable configuration or regular expressions.
type RuleSet struct {
	config   Config
	patterns compiledPatterns
}

type compiledPatterns struct {
	bootStart  *regexp.Regexp
	milestones []*regexp.Regexp
	forbidden  []*regexp.Regexp
}

// CompileRules validates cfg and compiles each expression once. The returned
// RuleSet owns a deep copy of the configuration; later changes to cfg cannot
// affect evaluation. Calling Config.Validate first is unnecessary.
func CompileRules(cfg Config) (*RuleSet, error) {
	patterns, err := validateConfig(cfg)
	if err != nil {
		return nil, err
	}
	owned := cfg
	owned.Milestones = append([]MilestoneConfig(nil), cfg.Milestones...)
	for i := range owned.Milestones {
		owned.Milestones[i].WithinMS = ruleIntCopy(owned.Milestones[i].WithinMS)
	}
	owned.Forbidden = append([]ForbiddenConfig(nil), cfg.Forbidden...)
	return &RuleSet{config: owned, patterns: patterns}, nil
}

func ruleIntCopy(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
