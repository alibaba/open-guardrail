package servercmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/alibaba/open-guardrail/internal/bootstrap"
	"github.com/alibaba/open-guardrail/internal/buildinfo"
	"github.com/alibaba/open-guardrail/internal/config"
)

// Main runs the command using process arguments and standard streams.
func Main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(ctx, os.Args[1:], os.Stdout, logger); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string, out io.Writer, logger *slog.Logger) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprintln(out, "Usage: open-guardrail serve [--config file] [--data-dir directory] [--listen host:port]\n       open-guardrail admin reset [--config file] [--data-dir directory]\n       open-guardrail version")
		return nil
	}
	if args[0] == "version" {
		fmt.Fprintln(out, buildinfo.Current().Version)
		return nil
	}
	if args[0] == "admin" {
		return resetAdmin(ctx, args[1:], os.Stdin, out)
	}
	if args[0] != "serve" {
		return errors.New("unknown command; use open-guardrail --help")
	}
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(out)
	path := flags.String("config", "", "YAML configuration file")
	dir := flags.String("data-dir", "", "SQLite data directory (default: ./data)")
	listen := flags.String("listen", "", "HTTP listen address (default: 127.0.0.1:8080)")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return errors.New("invalid command flags; use open-guardrail serve --help")
	}
	if flags.NArg() != 0 {
		return errors.New("serve does not accept positional arguments")
	}
	c, err := config.Load(*path, *listen, *dir)
	if err != nil {
		return err
	}
	return bootstrap.Serve(ctx, c, logger)
}
