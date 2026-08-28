package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// globalFlags holds the persistent flag values shared by every subcommand.
type globalFlags struct {
	format    string
	url       string
	team      string
	debug     bool
	configDir string
	cacheDir  string
	refresh   bool
}

// app is the resolved invocation state. Subcommands read it rather than
// threading configuration through every signature.
type app struct {
	cfg    *Config
	client *Client
	format Format
	out    io.Writer

	// teamID caches the resolved active team for the process lifetime.
	teamIDCache string
	teamName    string
}

var (
	gf globalFlags
	ap *app
)

var rootCmd = &cobra.Command{
	Use:   "pt",
	Short: "Query and manage your partrack parts library from the command line",
	Long: "pt searches your partrack catalog and inventory: parametric part search,\n" +
		"stock levels and locations, distributor listings, datasheets, and notes.\n" +
		"Output is plain and scriptable by default (--format json for a structured\n" +
		"backstop).",
	SilenceUsage:  true,
	SilenceErrors: true,
	Args:          cobra.ArbitraryArgs,
	// With no subcommand, print help. Cobra routes an unrecognised subcommand
	// here as a positional argument, which is how it becomes a usage error
	// (exit 2) rather than a generic failure.
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return exitErrf(ExitUsage, "unknown command %q — run `pt --help` for the command list", args[0])
		}
		return cmd.Help()
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		format, err := ParseFormat(gf.format)
		if err != nil {
			return exitErr(ExitUsage, err)
		}
		cfg, err := LoadConfig(Overrides{
			URL:       gf.url,
			Team:      gf.team,
			ConfigDir: gf.configDir,
			CacheDir:  gf.cacheDir,
		})
		if err != nil {
			return exitErr(ExitFailure, err)
		}
		ap = &app{cfg: cfg, format: format, out: cmd.OutOrStdout()}
		return nil
	},
}

// usageArgs wraps a positional-args validator so a failure exits with the usage
// code rather than the generic error code.
func usageArgs(fn cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := fn(cmd, args); err != nil {
			if hint := detachedFlagHint(cmd); hint != "" {
				return exitErr(ExitUsage, fmt.Errorf("%w — %s", err, hint))
			}
			return exitErr(ExitUsage, err)
		}
		return nil
	}
}

// detachedFlagHint explains the one way an argument count goes wrong by
// accident. A flag whose value is optional (--history, --params) only accepts
// an attached value, so `--history 20` takes the flag's default and leaves
// "20" behind as a stray positional. The bare count mismatch cobra reports for
// that is true but useless.
func detachedFlagHint(cmd *cobra.Command) string {
	var name string
	cmd.Flags().Visit(func(f *pflag.Flag) {
		if name == "" && f.NoOptDefVal != "" {
			name = f.Name
		}
	})
	if name == "" {
		return ""
	}
	return fmt.Sprintf("--%s takes its value attached, as --%s=<value>", name, name)
}

// requireClient lazily builds the API client. Commands that touch the network
// call this; `config` and `--help` never do.
func (a *app) requireClient() (*Client, error) {
	if a.client != nil {
		return a.client, nil
	}
	c, err := NewClient(a.cfg, gf.debug)
	if err != nil {
		return nil, err
	}
	a.client = c
	return c, nil
}

// requireAuthedClient builds the client and confirms a token is present.
func (a *app) requireAuthedClient() (*Client, error) {
	c, err := a.requireClient()
	if err != nil {
		return nil, err
	}
	if err := c.requireToken(); err != nil {
		return nil, err
	}
	return c, nil
}

func init() {
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return exitErr(ExitUsage, err)
	})

	pf := rootCmd.PersistentFlags()
	pf.StringVar(&gf.format, "format", "plain", "output format: plain or json")
	pf.StringVar(&gf.url, "url", "", "partrack server base URL (default from config or PARTRACK_URL)")
	pf.StringVar(&gf.team, "team", "", "team name or id to act as (default from config or PARTRACK_TEAM)")
	pf.BoolVarP(&gf.debug, "debug", "v", false, "log HTTP requests to stderr")
	pf.BoolVar(&gf.refresh, "refresh", false, "bypass the local reference-data cache")
	pf.StringVar(&gf.configDir, "config-dir", "", "override the config directory")
	pf.StringVar(&gf.cacheDir, "cache-dir", "", "override the cache directory")

	rootCmd.AddCommand(
		bomCmd,
		authCmd,
		configCmd,
		teamsCmd,
		searchCmd,
		filtersCmd,
		categoriesCmd,
		showCmd,
		stockCmd,
		whereCmd,
		projectsCmd,
		ordersCmd,
		buildsCmd,
		cartCmd,
		checkoutsCmd,
	)
}

// Execute runs the command tree and returns a process exit code.
func Execute() int {
	err := rootCmd.Execute()
	if err == nil {
		return ExitOK
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		fmt.Fprintln(os.Stderr, "pt: "+ee.Err.Error())
		return ee.Code
	}
	fmt.Fprintln(os.Stderr, "pt: "+err.Error())
	return ExitFailure
}
