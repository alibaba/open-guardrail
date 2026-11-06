package servercmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/alibaba/open-guardrail/internal/adapters/security"
	"github.com/alibaba/open-guardrail/internal/adapters/storage"
	"github.com/alibaba/open-guardrail/internal/application/identity"
	"github.com/alibaba/open-guardrail/internal/config"
	"golang.org/x/term"
)

func resetAdmin(ctx context.Context, args []string, input *os.File, out io.Writer) error {
	if len(args) == 0 || args[0] == "--help" {
		fmt.Fprintln(out, "Usage: open-guardrail admin reset [--config file] [--data-dir directory]")
		return nil
	}
	if args[0] != "reset" {
		return errors.New("unknown admin command; use open-guardrail admin --help")
	}
	flags := flag.NewFlagSet("admin reset", flag.ContinueOnError)
	flags.SetOutput(out)
	path := flags.String("config", "", "Service YAML configuration file")
	dir := flags.String("data-dir", "", "Existing SQLite data directory (default: ./data)")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errors.New("invalid flags; use open-guardrail admin reset --help")
	}
	if flags.NArg() != 0 {
		return errors.New("admin reset does not accept positional arguments")
	}
	if !term.IsTerminal(int(input.Fd())) {
		return errors.New("admin reset requires an interactive terminal; passwords cannot be supplied as arguments or piped input")
	}
	c, err := config.Load(*path, "", *dir)
	if err != nil {
		return err
	}
	openCtx, cancel := context.WithTimeout(ctx, config.StartupTimeout)
	store, err := storage.OpenExisting(openCtx, c)
	cancel()
	if err != nil {
		return err
	}
	defer store.Close()
	svc := identity.New(store.Identity(), security.Passwords{})
	checkCtx, cancel := context.WithTimeout(ctx, config.StartupTimeout)
	initialized, err := svc.Status(checkCtx)
	cancel()
	if err != nil {
		return err
	}
	if !initialized {
		return errors.New("no initialized administrator found; check the service database configuration")
	}
	if c.Database.Driver == "sqlite" {
		fmt.Fprintf(out, "Database: %s\n", filepath.Join(c.DataDir, "guardrail.db"))
	} else {
		fmt.Fprintln(out, "Database: configured PostgreSQL database (connection string hidden)")
	}
	fmt.Fprintln(out, "Reset the existing administrator. All administrator sessions will be revoked. Agent keys and policy data will be preserved.")
	username, password, err := readAdminReset(ctx, input, out)
	if err != nil {
		return err
	}
	resetCtx, cancel := context.WithTimeout(ctx, config.StartupTimeout)
	defer cancel()
	if err = svc.ResetLocalAdmin(resetCtx, username, password); err != nil {
		if errors.Is(err, identity.ErrInvalid) {
			return errors.New("invalid username or password; username must be 3-64 letters, digits, dots, underscores or hyphens, starting with a letter or digit")
		}
		return err
	}
	fmt.Fprintln(out, "Administrator reset. Sign in with the new credentials; existing administrator sessions are no longer valid.")
	return nil
}

func readAdminReset(ctx context.Context, input *os.File, out io.Writer) (string, string, error) {
	fd := int(input.Fd())
	state, err := term.GetState(fd)
	if err != nil {
		return "", "", errors.New("cannot read terminal state")
	}
	// Also restore echo when Ctrl+C cancels a blocked password read.
	defer term.Restore(fd, state)
	type result struct {
		username, password string
		err                error
	}
	done := make(chan result, 1)
	go func() {
		fmt.Fprint(out, "Type RESET to continue: ")
		confirmation, err := readResetLine(input)
		if err != nil || strings.TrimSpace(confirmation) != "RESET" {
			done <- result{err: errors.New("reset cancelled")}
			return
		}
		fmt.Fprint(out, "New username (Enter to keep current): ")
		username, err := readResetLine(input)
		if err != nil {
			done <- result{err: errors.New("cannot read username")}
			return
		}
		fmt.Fprintln(out, "Password: at least 6 characters, at most 128 UTF-8 bytes.")
		if ctx.Err() != nil {
			done <- result{err: errors.New("reset cancelled")}
			return
		}
		fmt.Fprint(out, "New password: ")
		password, err := term.ReadPassword(fd)
		fmt.Fprintln(out)
		if err != nil {
			done <- result{err: errors.New("cannot read password")}
			return
		}
		defer clear(password)
		if ctx.Err() != nil {
			done <- result{err: errors.New("reset cancelled")}
			return
		}
		fmt.Fprint(out, "Confirm password: ")
		confirmationBytes, err := term.ReadPassword(fd)
		fmt.Fprintln(out)
		defer clear(confirmationBytes)
		if err != nil {
			done <- result{err: errors.New("cannot read password confirmation")}
			return
		}
		if string(password) != string(confirmationBytes) {
			done <- result{err: errors.New("passwords do not match; nothing changed")}
			return
		}
		if !identity.ValidPassword(string(password)) {
			done <- result{err: errors.New("password must contain at least 6 characters and at most 128 UTF-8 bytes; nothing changed")}
			return
		}
		done <- result{username: strings.TrimSpace(username), password: string(password)}
	}()
	select {
	case <-ctx.Done():
		return "", "", errors.New("reset cancelled")
	case result := <-done:
		return result.username, result.password, result.err
	}
}

// Avoid buffering ahead into the subsequent hidden password prompt.
func readResetLine(input io.Reader) (string, error) {
	var line strings.Builder
	var ch [1]byte
	for line.Len() <= 256 {
		if _, err := io.ReadFull(input, ch[:]); err != nil {
			return "", err
		}
		if ch[0] == '\n' {
			return strings.TrimSuffix(line.String(), "\r"), nil
		}
		line.WriteByte(ch[0])
	}
	return "", errors.New("input is too long")
}
