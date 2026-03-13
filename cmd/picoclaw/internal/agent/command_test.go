package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAgentCommand(t *testing.T) {
	cmd := NewAgentCommand()

	require.NotNil(t, cmd)

	assert.Equal(t, "agent", cmd.Use)
	assert.Equal(t, "Interact with the agent directly", cmd.Short)

	assert.Len(t, cmd.Aliases, 0)
	assert.True(t, cmd.HasSubCommands())

	assert.Nil(t, cmd.Run)
	assert.NotNil(t, cmd.RunE)

	assert.Nil(t, cmd.PersistentPreRun)
	assert.Nil(t, cmd.PersistentPostRun)

	assert.True(t, cmd.HasFlags())

	assert.NotNil(t, cmd.Flags().Lookup("debug"))
	assert.NotNil(t, cmd.Flags().Lookup("message"))
	assert.NotNil(t, cmd.Flags().Lookup("session"))
	assert.NotNil(t, cmd.Flags().Lookup("model"))

	syncCmd, _, err := cmd.Find([]string{"sync-defaults"})
	require.NoError(t, err)
	require.NotNil(t, syncCmd)
	assert.Equal(t, "sync-defaults", syncCmd.Name())
	assert.NotNil(t, syncCmd.Flags().Lookup("dry-run"))
	assert.NotNil(t, syncCmd.Flags().Lookup("force-legacy"))
}
