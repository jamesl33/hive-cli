package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"bogus"},
		{"manual"},
		{"manual", "99"},
		{"boost"},
		{"boost", "7h"},
		{"dashboard", "extra"},
	} {
		rootCommand.SetArgs(args)
		assert.Error(t, rootCommand.ExecuteContext(t.Context()), "%v", args)
	}
}
