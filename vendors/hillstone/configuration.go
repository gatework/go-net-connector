package hillstone

import (
	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/vendors/internal/driver"
)

type configPolicy struct{ driver.BaseConfigPolicy }

var pagingPattern = driver.Regex(`\x00?\x08+[ \t]+\x08+`)

func (p configPolicy) Clean(step connector.StepResult, _, _ string) (string, error) {
	output, err := step.Response().Output()
	if err != nil {
		return "", err
	}
	return cleanOutput(string(output))
}

func cleanOutput(output string) (string, error) {
	return pagingPattern.ReplaceAllString(output, ""), nil
}

func configFactory() func() connector.ConfigStrategy {
	return func() connector.ConfigStrategy {
		return driver.NewConfigStrategy(configPolicy{})
	}
}
