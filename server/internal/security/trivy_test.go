package security

import "testing"

func TestParseTrivySeveritySummary(t *testing.T) {
	raw := []byte(`{"Results":[{"Vulnerabilities":[{"Severity":"CRITICAL"},{"Severity":"HIGH"},{"Severity":"LOW"},{"Severity":"BOGUS"}]}]}`)
	summary, err := ParseTrivySeveritySummary(raw)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Critical != 1 || summary.High != 1 || summary.Low != 1 || summary.Unknown != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}
}
