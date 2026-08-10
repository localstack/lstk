package cmd

import (
	"testing"
)

// Only the flag counts as a selection: `lstk sam` uses the signal to decide
// whether to put --region on sam's own command line, and an ambient AWS_REGION
// must not start overriding a project's samconfig.toml.
func TestResolveRegionSelection(t *testing.T) {
	t.Setenv("AWS_REGION", "")

	region, selected := resolveRegionSelection("us-west-2")
	if region != "us-west-2" || !selected {
		t.Errorf("flag: got (%q, %v), want (us-west-2, true)", region, selected)
	}

	region, selected = resolveRegionSelection("")
	if region != "us-east-1" || selected {
		t.Errorf("default: got (%q, %v), want (us-east-1, false)", region, selected)
	}

	t.Setenv("AWS_REGION", "eu-central-1")
	region, selected = resolveRegionSelection("")
	if region != "eu-central-1" || selected {
		t.Errorf("env is used but is not a selection: got (%q, %v)", region, selected)
	}

	region, selected = resolveRegionSelection("ap-south-1")
	if region != "ap-south-1" || !selected {
		t.Errorf("flag over env: got (%q, %v)", region, selected)
	}
}

func TestResolveRegion(t *testing.T) {
	t.Setenv("AWS_REGION", "")
	if got := resolveRegion("us-west-2"); got != "us-west-2" {
		t.Errorf("flag should win: got %q", got)
	}
	if got := resolveRegion(""); got != "us-east-1" {
		t.Errorf("default should be us-east-1: got %q", got)
	}
	t.Setenv("AWS_REGION", "eu-central-1")
	if got := resolveRegion(""); got != "eu-central-1" {
		t.Errorf("env fallback: got %q", got)
	}
	if got := resolveRegion("ap-south-1"); got != "ap-south-1" {
		t.Errorf("flag over env: got %q", got)
	}
}
