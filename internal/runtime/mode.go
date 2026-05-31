package runtime

import (
	"errors"
	"fmt"
	"strings"
)

type Mode string

const (
	Development Mode = "development"
	Production  Mode = "production"
)

func ResolveMode(developmentFlag bool, productionFlag bool, envMode string) (Mode, error) {
	if developmentFlag && productionFlag {
		return "", errors.New("--development and --production are mutually exclusive")
	}
	if developmentFlag {
		return Development, nil
	}
	if productionFlag {
		return Production, nil
	}

	normalized := strings.ToLower(strings.TrimSpace(envMode))
	if normalized == "" {
		return Production, nil
	}

	switch Mode(normalized) {
	case Development:
		return Development, nil
	case Production:
		return Production, nil
	default:
		return "", fmt.Errorf("invalid PODOREL_MODE %q: expected development or production", envMode)
	}
}

func (m Mode) String() string {
	return string(m)
}

func (m Mode) IsDevelopment() bool {
	return m == Development
}

func (m Mode) IsProduction() bool {
	return m == Production
}

func (m Mode) Validate() error {
	switch m {
	case Development, Production:
		return nil
	default:
		return fmt.Errorf("invalid runtime mode %q", m)
	}
}
