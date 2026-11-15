// Package bootlog parses and evaluates deterministic checks of one saved
// boot log against project-defined rules. A passing report describes the supplied
// capture only; it cannot establish device health or capture completeness.
//
// Times are checked signed 64-bit integer nanoseconds, never floating point.
// Nil timestamp pointers mean unknown time; a pointer to zero means actual zero.
// Milestone deadlines use integer milliseconds, with nil meaning presence-only.
// Log.Validate, Config.Validate, and Report.Validate check caller-built values.
// ParseOptions.Normalize supplies defaults without changing caller options.
// Report validation does not turn failed checks into operational errors.
//
// Readers and writers remain caller-owned. The parser checks cancellation
// between reads, but cannot interrupt an arbitrary reader blocked inside Read.
// The evaluator is synchronous, finite, and bounded by validated input
// and rule counts; it does not accept a context or change either input.
// The compiled RuleSet owns copied configuration and private regular expressions
// and supports concurrent evaluations. Its zero value is invalid; use
// CompileRules to construct it.
//
// ParseLog and ParseRules read caller-supplied input. CompileRules validates and
// compiles configuration. Evaluate returns check results, including failed checks
// as report data. The separate report package renders text, JSON, and JUnit.
//
// Messages remain exact valid UTF-8, including control characters. Output writers
// escape unsafe display characters without silently deleting them. Evidence
// can contain sensitive source-log data and follows the same data-handling rules.
package bootlog
