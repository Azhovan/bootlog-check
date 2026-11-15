package report

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/firmdura/bootlog-check"
)

type junitSuite struct {
	XMLName  xml.Name    `xml:"testsuite"`
	Name     string      `xml:"name,attr"`
	Tests    int         `xml:"tests,attr"`
	Failures int         `xml:"failures,attr"`
	Skipped  int         `xml:"skipped,attr"`
	Errors   int         `xml:"errors,attr"`
	Cases    []junitCase `xml:"testcase"`
	Output   string      `xml:"system-out"`
}

type junitCase struct {
	Name      string        `xml:"name,attr"`
	Classname string        `xml:"classname,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
	Skipped   *junitSkipped `xml:"skipped,omitempty"`
	Output    string        `xml:"system-out"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Type    string `xml:"type,attr"`
	Text    string `xml:",chardata"`
}

type junitSkipped struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

func junitReport(r bootlog.Report) ([]byte, error) {
	var overview strings.Builder
	writeOverview(&overview, r)
	fmt.Fprintf(&overview, "scope: %s\n", r.Scope)
	suite := junitSuite{
		Name: "bootlog-check", Tests: r.Summary.Total,
		Failures: r.Summary.Failed, Skipped: r.Summary.NotEvaluated,
		Cases: make([]junitCase, 0, len(r.Checks)), Output: overview.String(),
	}
	for _, check := range r.Checks {
		details := checkDetails(check)
		c := junitCase{Name: check.ID, Classname: "bootlog." + string(check.Kind), Output: details}
		switch check.Status {
		case bootlog.StatusFailed:
			c.Failure = &junitFailure{Message: string(check.Reason), Type: string(check.Reason), Text: details}
		case bootlog.StatusNotEvaluated:
			c.Skipped = &junitSkipped{Message: string(check.Reason), Text: details}
		}
		suite.Cases = append(suite.Cases, c)
	}
	data, err := xml.MarshalIndent(suite, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(append([]byte(xml.Header), data...), '\n'), nil
}
