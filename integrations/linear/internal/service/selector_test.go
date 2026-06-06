//nolint:goconst // Repeated status strings make selector cases explicit.
package service

import (
	"testing"

	"github.com/bastion-computer/bastion/integrations/linear/internal/bastion"
)

func TestSelectorMatchesRunningEnvironmentByPatterns(t *testing.T) {
	t.Parallel()

	key := "linear-worker-1"
	selector := Selector{IDPatterns: []string{"env_*"}, KeyPatterns: []string{"linear-*"}}

	if !selector.Match(bastion.Environment{ID: "env_123", Key: &key, Status: "running"}) {
		t.Fatalf("selector did not match running environment")
	}
}

func TestSelectorRejectsNonRunningEnvironment(t *testing.T) {
	t.Parallel()

	selector := Selector{IDPatterns: []string{"env_*"}}
	if selector.Match(bastion.Environment{ID: "env_123", Status: "error"}) {
		t.Fatalf("selector matched non-running environment")
	}
}

func TestSelectorRejectsMissingKeyWhenKeyPatternConfigured(t *testing.T) {
	t.Parallel()

	selector := Selector{KeyPatterns: []string{"linear-*"}}
	if selector.Match(bastion.Environment{ID: "env_123", Status: "running"}) {
		t.Fatalf("selector matched environment without key")
	}
}
