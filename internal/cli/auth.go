package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// userResponse mirrors the server's user object.
type userResponse struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	IsAdmin     bool   `json:"is_admin"`
}

type membership struct {
	TeamID   string `json:"team_id"`
	TeamName string `json:"team_name"`
	Role     string `json:"role"`
}

type meResponse struct {
	User        userResponse `json:"user"`
	Memberships []membership `json:"memberships"`
}

type tokenResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Token     string `json:"token"`
	CreatedAt string `json:"created_at"`
	ExpiresAt string `json:"expires_at"`
}

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage credentials for a partrack server",
}

var (
	loginEmail     string
	loginTokenPast string
	loginTokenName string
)

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate and store an API token",
	Long: "Prompts for your email and password, mints a named API token, and stores it\n" +
		"with 0600 permissions. The password itself is never written to disk and the\n" +
		"session used to mint the token is closed immediately.\n\n" +
		"If you sign in through SSO or LDAP and have no local password, create a token\n" +
		"in the web UI and pass it with --token instead.",
	Args: usageArgs(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAuthLogin(cmd.Context())
	},
}

func runAuthLogin(ctx context.Context) error {
	client, err := ap.requireClient()
	if err != nil {
		return err
	}
	client.Token = ""

	name := loginTokenName
	if name == "" {
		host, err := os.Hostname()
		if err != nil || host == "" {
			host = "cli"
		}
		name = "pt@" + host
	}

	var creds credentials
	creds.URL = ap.cfg.URL

	if loginTokenPast != "" {
		// Pasted token: verify it works and record who it belongs to.
		client.Token = strings.TrimSpace(loginTokenPast)
		var me meResponse
		if err := client.Get(ctx, "/auth/me", nil, &me); err != nil {
			return err
		}
		creds.Token = client.Token
		creds.User = me.User.Email
	} else {
		email := loginEmail
		if email == "" {
			email, err = promptLine("Email: ")
			if err != nil {
				return exitErr(ExitUsage, err)
			}
		}
		password, err := promptSecret("Password: ")
		if err != nil {
			return exitErr(ExitUsage, err)
		}
		if email == "" || password == "" {
			return exitErrf(ExitUsage, "email and password are required")
		}

		// Cookie session first — POST /tokens needs one to mint against.
		var login struct {
			User userResponse `json:"user"`
		}
		if err := client.postForSession(ctx,
			"/auth/login", map[string]string{"email": email, "password": password}, &login); err != nil {
			return err
		}
		if !client.hasSession() {
			return exitErrf(ExitAuth, "login succeeded but no session cookie was returned")
		}

		var tok tokenResponse
		if err := client.postForSession(ctx,
			"/tokens", map[string]any{"name": name}, &tok); err != nil {
			return err
		}
		// The session has done its job; drop it so only the token remains.
		if err := client.postForSession(ctx, "/auth/logout", nil, nil); err != nil && gf.debug {
			fmt.Fprintf(os.Stderr, "pt: session logout failed: %v\n", err)
		}

		creds.Token = tok.Token
		creds.ID = tok.ID
		creds.User = login.User.Email
		client.Token = tok.Token
	}

	if err := ap.cfg.saveCredentials(creds); err != nil {
		return exitErr(ExitFailure, err)
	}
	ap.cfg.Token = creds.Token

	// A single-team user gets their team recorded now, so no --team is needed.
	var me meResponse
	if err := client.Get(ctx, "/auth/me", nil, &me); err == nil {
		if ap.cfg.Team == "" && len(me.Memberships) == 1 {
			ap.cfg.Team = me.Memberships[0].TeamName
		}
	}
	if err := ap.cfg.Save(); err != nil {
		return exitErr(ExitFailure, err)
	}

	if ap.format == FormatJSON {
		return writeJSON(ap.out, map[string]any{
			"authenticated": true,
			"url":           ap.cfg.URL,
			"user":          creds.User,
			"token_name":    name,
			"team":          ap.cfg.Team,
		})
	}
	fmt.Fprintf(ap.out, "Authenticated to %s as %s\n", ap.cfg.URL, creds.User)
	if ap.cfg.Team != "" {
		fmt.Fprintf(ap.out, "Active team: %s\n", ap.cfg.Team)
	} else if len(me.Memberships) > 1 {
		fmt.Fprintf(ap.out, "You belong to %d teams — set one with `pt config set team <name>`\n", len(me.Memberships))
	}
	return nil
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the current credential and server",
	Args:  usageArgs(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		if ap.cfg.Token == "" {
			if ap.format == FormatJSON {
				return writeJSON(ap.out, map[string]any{"authenticated": false, "url": ap.cfg.URL})
			}
			writeKV(ap.out, [][2]string{
				{"Authenticated", "no"},
				{"Server", strOr(&ap.cfg.URL, dash)},
			})
			return exitErrf(ExitAuth, "no stored credentials — run `pt auth login`")
		}

		client, err := ap.requireAuthedClient()
		if err != nil {
			return err
		}
		var me meResponse
		if err := client.Get(ctx, "/auth/me", nil, &me); err != nil {
			return err
		}

		if ap.format == FormatJSON {
			return writeJSON(ap.out, map[string]any{
				"authenticated": true,
				"url":           ap.cfg.URL,
				"token_source":  ap.cfg.TokenSource,
				"user":          me.User,
				"memberships":   me.Memberships,
				"team":          ap.cfg.Team,
			})
		}

		pairs := [][2]string{
			{"Authenticated", "yes"},
			{"Server", ap.cfg.URL},
			{"User", fmt.Sprintf("%s <%s>", me.User.DisplayName, me.User.Email)},
			{"Token source", ap.cfg.TokenSource},
			{"Active team", strOr(&ap.cfg.Team, "(none set)")},
		}
		if me.User.IsAdmin {
			pairs = append(pairs, [2]string{"Instance admin", "yes"})
		}
		writeKV(ap.out, pairs)
		if len(me.Memberships) > 0 {
			writeSection(ap.out, "TEAMS")
			rows := make([][]string, len(me.Memberships))
			for i, m := range me.Memberships {
				rows[i] = []string{m.TeamName, m.Role}
			}
			writeTable(ap.out, nil, rows)
		}
		return nil
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Revoke and remove the stored API token",
	Args:  usageArgs(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		var creds credentials
		if err := readJSONFile(ap.cfg.ConfigDir+"/"+credsFileName, &creds); err != nil {
			return exitErr(ExitFailure, err)
		}
		if creds.Token == "" {
			fmt.Fprintln(ap.out, "No stored credentials.")
			return nil
		}
		// Revoke server-side when we know the token id; a failure here (server
		// down, token already gone) must not block clearing the local file.
		if creds.ID != "" {
			if client, err := ap.requireClient(); err == nil {
				client.Token = creds.Token
				if err := client.Delete(ctx, "/tokens/"+creds.ID); err != nil {
					fmt.Fprintf(os.Stderr, "pt: could not revoke token server-side: %v\n", err)
				}
			}
		}
		if err := ap.cfg.clearCredentials(); err != nil {
			return exitErr(ExitFailure, err)
		}
		fmt.Fprintln(ap.out, "Credentials removed.")
		return nil
	},
}

// promptLine reads one line from the terminal, writing the prompt to stderr so
// it never contaminates piped stdout.
func promptLine(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// promptSecret reads a line with echo disabled. When stdin is not a terminal
// (a pipe in CI), it falls back to a plain read.
func promptSecret(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return promptLine(prompt)
	}
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func init() {
	authLoginCmd.Flags().StringVar(&loginEmail, "email", "", "email to log in with (prompted when omitted)")
	authLoginCmd.Flags().StringVar(&loginTokenPast, "token", "", "store an existing ptk_ token instead of logging in")
	authLoginCmd.Flags().StringVar(&loginTokenName, "token-name", "", "name for the minted token (default pt@hostname)")

	authCmd.AddCommand(authLoginCmd, authStatusCmd, authLogoutCmd)
}
