package security

import "encoding/json"

type SeveritySummary struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Unknown  int `json:"unknown"`
}

type TrivyFinding struct {
	Target           string `json:"target"`
	VulnerabilityID  string `json:"vulnerability_id"`
	Severity         string `json:"severity"`
	Title            string `json:"title"`
	PackageName      string `json:"package_name"`
	InstalledVersion string `json:"installed_version"`
	FixedVersion     string `json:"fixed_version"`
	RawJSON          string `json:"raw_json"`
}

type trivyReport struct {
	Results []struct {
		Target          string `json:"Target"`
		Vulnerabilities []struct {
			VulnerabilityID  string `json:"VulnerabilityID"`
			Severity         string `json:"Severity"`
			Title            string `json:"Title"`
			PkgName          string `json:"PkgName"`
			InstalledVersion string `json:"InstalledVersion"`
			FixedVersion     string `json:"FixedVersion"`
		} `json:"Vulnerabilities"`
	} `json:"Results"`
}

func ParseTrivySeveritySummary(raw []byte) (SeveritySummary, error) {
	var report trivyReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return SeveritySummary{}, err
	}
	var summary SeveritySummary
	for _, result := range report.Results {
		for _, vulnerability := range result.Vulnerabilities {
			switch vulnerability.Severity {
			case "CRITICAL":
				summary.Critical++
			case "HIGH":
				summary.High++
			case "MEDIUM":
				summary.Medium++
			case "LOW":
				summary.Low++
			default:
				summary.Unknown++
			}
		}
	}
	return summary, nil
}

func ParseTrivyFindings(raw []byte) ([]TrivyFinding, error) {
	var report trivyReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, err
	}
	var findings []TrivyFinding
	for _, result := range report.Results {
		for _, vulnerability := range result.Vulnerabilities {
			rawFinding, _ := json.Marshal(vulnerability)
			findings = append(findings, TrivyFinding{
				Target:           result.Target,
				VulnerabilityID:  vulnerability.VulnerabilityID,
				Severity:         vulnerability.Severity,
				Title:            vulnerability.Title,
				PackageName:      vulnerability.PkgName,
				InstalledVersion: vulnerability.InstalledVersion,
				FixedVersion:     vulnerability.FixedVersion,
				RawJSON:          string(rawFinding),
			})
		}
	}
	return findings, nil
}
