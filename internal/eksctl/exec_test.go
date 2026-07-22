package eksctl

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsHelp(t *testing.T) {
	for _, args := range [][]string{
		{"--help"}, {"-h"}, {"help"}, {"create", "cluster", "--help"}, {"get", "clusters", "-h"},
	} {
		assert.Truef(t, IsHelp(args), "%v", args)
	}
	for _, args := range [][]string{{"create", "cluster"}, {"get", "clusters"}, {}} {
		assert.Falsef(t, IsHelp(args), "%v", args)
	}
}

func TestIsOffline(t *testing.T) {
	offline := [][]string{
		{"version"},
		{"info"},
		{"completion", "bash"},
		{"help"},
		{"--help"},
		{"-h"},
		{"create", "cluster", "--help"},
	}
	for _, args := range offline {
		assert.Truef(t, IsOffline(args), "expected %v offline", args)
	}

	awsContacting := [][]string{
		{"create", "cluster", "--nodes", "1"},
		{"get", "clusters"},
		{"delete", "cluster", "--name", "demo"},
		{"upgrade", "cluster"},
		{}, // no subcommand → not offline (gate on emulator)
	}
	for _, args := range awsContacting {
		assert.Falsef(t, IsOffline(args), "expected %v not offline", args)
	}
}

func TestSubcommandSkipsLeadingFlags(t *testing.T) {
	assert.Equal(t, "create", subcommand([]string{"-v", "4", "create", "cluster"}))
	assert.Equal(t, "get", subcommand([]string{"--color=false", "get", "clusters"}))
	assert.Equal(t, "", subcommand([]string{"-h"}))
	assert.Equal(t, "", subcommand(nil))
}
