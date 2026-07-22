package cmd

import (
	"testing"

	"github.com/localstack/lstk/internal/env"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mergeTestCmd builds a bare *cobra.Command with just the --merge flag
// registered (via the same addMergeFlag used by the real load commands), so
// resolveLoadStrategy sees real cobra flag/Changed() behavior rather than
// hand-constructed booleans.
func mergeTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "load"}
	addMergeFlag(cmd)
	return cmd
}

// TestResolveLoadStrategy exercises the exact glue runSnapshotLoad uses to go
// from "what the user/environment configured" to "the strategy string handed
// to snapshot.LoadLocal/LoadPod" — real cobra flag parsing, cfg.MergeStrategy
// (the field env.Init() populates from LSTK_MERGE_STRATEGY), and
// snapshot.ValidateMergeStrategy — with no emulator or network involved.
// internal/snapshot's own tests separately cover that a given strategy string
// then produces the right client calls; this test covers the seam in between.
func TestResolveLoadStrategy(t *testing.T) {
	t.Run("no flag, no env: default strategy", func(t *testing.T) {
		cmd := mergeTestCmd()
		strategy, err := resolveLoadStrategy(cmd, &env.Env{})
		require.NoError(t, err)
		assert.Equal(t, "account-region-merge", strategy)
	})

	t.Run("env var alone sets the strategy", func(t *testing.T) {
		cmd := mergeTestCmd()
		strategy, err := resolveLoadStrategy(cmd, &env.Env{MergeStrategy: "overwrite"})
		require.NoError(t, err)
		assert.Equal(t, "overwrite", strategy)
	})

	t.Run("explicit flag overrides env var", func(t *testing.T) {
		cmd := mergeTestCmd()
		require.NoError(t, cmd.Flags().Parse([]string{"--merge=account-region-merge"}))
		strategy, err := resolveLoadStrategy(cmd, &env.Env{MergeStrategy: "overwrite"})
		require.NoError(t, err)
		assert.Equal(t, "account-region-merge", strategy)
	})

	t.Run("invalid env var value is rejected", func(t *testing.T) {
		cmd := mergeTestCmd()
		_, err := resolveLoadStrategy(cmd, &env.Env{MergeStrategy: "bogus"})
		assert.ErrorContains(t, err, "unknown merge strategy")
	})
}
