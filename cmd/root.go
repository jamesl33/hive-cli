package cmd

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/jamesl33/hive-cli/cmd/boost"
	"github.com/jamesl33/hive-cli/cmd/dashboard"
	"github.com/jamesl33/hive-cli/cmd/manual"
	"github.com/jamesl33/hive-cli/cmd/off"
	"github.com/jamesl33/hive-cli/cmd/record"
	"github.com/jamesl33/hive-cli/cmd/schedule"
	"github.com/jamesl33/hive-cli/cmd/status"
	"github.com/jamesl33/hive-cli/internal/z2m"
	"github.com/spf13/cobra"
)

// version is the release version.
const version = "v0.1.0"

// commit is the Git SHA, set at build time using '-ldflags "-X github.com/jamesl33/hive-cli/cmd.commit=..."'.
var commit = "unknown"

// options are the options shared by every command which talks to Zigbee2MQTT.
var options z2m.Options

// rootCommand defines the root of the command chain.
var rootCommand = &cobra.Command{
	Short:         "Local control of a Hive thermostat through Zigbee2MQTT.",
	Use:           "hive",
	Version:       version + " (" + commit + ")",
	Args:          cobra.NoArgs,
	RunE:          func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	SilenceErrors: true,
	SilenceUsage:  true,
}

// init sets up the CLI.
func init() {
	flags := rootCommand.PersistentFlags()

	flags.StringVar(
		&options.Broker,
		"broker",
		z2m.Env("HIVE_BROKER", "tcp://localhost:1883"),
		"MQTT broker URL, tcp:// or mqtts:// (HIVE_BROKER)",
	)

	flags.StringVar(
		&options.Username,
		"username",
		z2m.Env("HIVE_USERNAME", ""),
		"MQTT username (HIVE_USERNAME)",
	)

	flags.StringVar(
		&options.Password,
		"password",
		z2m.Env("HIVE_PASSWORD", ""),
		"MQTT password (HIVE_PASSWORD)",
	)

	rootCommand.AddCommand(
		boost.NewBoost(&options),
		dashboard.NewDashboard(&options),
		manual.NewManual(&options),
		off.NewOff(&options),
		record.NewRecord(&options),
		schedule.NewSchedule(&options),
		status.NewStatus(&options),
	)
}

// Execute 'hive'.
func Execute() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	err := rootCommand.ExecuteContext(ctx)
	if err == nil {
		return nil
	}

	return err // Purposefully not wrapped
}
