package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "View and edit stored settings",
}

// configKeys are the settings `pt config set` accepts. The token is not among
// them — it lives in a separate 0600 file managed by `pt auth`.
var configKeys = []string{"url", "team"}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the resolved configuration",
	Args:  usageArgs(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		if ap.format == FormatJSON {
			return writeJSON(ap.out, map[string]any{
				"url":             ap.cfg.URL,
				"team":            ap.cfg.Team,
				"authenticated":   ap.cfg.Token != "",
				"token_source":    ap.cfg.TokenSource,
				"config_dir":      ap.cfg.ConfigDir,
				"cache_dir":       ap.cfg.CacheDir,
				"config_file":     ap.cfg.configPath(),
				"output_format":   string(ap.format),
				"param_tolerance": defaultParamTolerancePct,
			})
		}
		writeKV(ap.out, [][2]string{
			{"url", strOr(&ap.cfg.URL, "(unset)")},
			{"team", strOr(&ap.cfg.Team, "(unset)")},
			{"authenticated", yesNo(ap.cfg.Token != "")},
			{"token source", strOr(&ap.cfg.TokenSource, dash)},
			{"config dir", ap.cfg.ConfigDir},
			{"cache dir", ap.cfg.CacheDir},
		})
		return nil
	},
}

var configPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the config file path",
	Args:  usageArgs(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(ap.out, ap.cfg.configPath())
		return nil
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Print one setting",
	Args:  usageArgs(cobra.ExactArgs(1)),
	RunE: func(cmd *cobra.Command, args []string) error {
		v, err := ap.cfg.get(args[0])
		if err != nil {
			return err
		}
		fmt.Fprintln(ap.out, v)
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Change one setting",
	Long:  "Settings: " + strings.Join(configKeys, ", "),
	Args:  usageArgs(cobra.ExactArgs(2)),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := ap.cfg.set(args[0], args[1]); err != nil {
			return err
		}
		if err := ap.cfg.Save(); err != nil {
			return exitErr(ExitFailure, err)
		}
		// Echo the stored value, not the input — set() normalizes (a URL loses
		// its trailing slash), and the user should see what was actually kept.
		stored, err := ap.cfg.get(args[0])
		if err != nil {
			return err
		}
		fmt.Fprintf(ap.out, "%s = %s\n", strings.ToLower(args[0]), stored)
		return nil
	},
}

var configClearCacheCmd = &cobra.Command{
	Use:   "clear-cache",
	Short: "Drop the cached category and parameter registry",
	Long: "pt caches only reference data — the category tree and the parameter\n" +
		"registry — plus the last result set for @N addressing. Stock, price and\n" +
		"location are never cached. This removes both files.",
	Args: usageArgs(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		removed := 0
		for _, p := range []string{ap.refDataPath(), ap.statePath()} {
			if err := os.Remove(p); err == nil {
				removed++
			} else if !os.IsNotExist(err) {
				return exitErr(ExitFailure, err)
			}
		}
		fmt.Fprintf(ap.out, "Removed %d cache file%s.\n", removed, plural(removed))
		return nil
	},
}

func (c *Config) get(key string) (string, error) {
	switch strings.ToLower(key) {
	case "url":
		return c.URL, nil
	case "team":
		return c.Team, nil
	default:
		return "", exitErrf(ExitUsage, "unknown setting %q — available: %s", key, strings.Join(configKeys, ", "))
	}
}

func (c *Config) set(key, value string) error {
	switch strings.ToLower(key) {
	case "url":
		c.URL = strings.TrimRight(value, "/")
	case "team":
		c.Team = value
	default:
		return exitErrf(ExitUsage, "unknown setting %q — available: %s", key, strings.Join(configKeys, ", "))
	}
	return nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func init() {
	configCmd.AddCommand(configShowCmd, configPathCmd, configGetCmd, configSetCmd, configClearCacheCmd)
}
