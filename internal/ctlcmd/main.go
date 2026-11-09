// guardrailctl operates an existing server exclusively through HTTP.
package ctlcmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

const diagnosticResponseLimit = 1 << 20

// Main runs the command using process arguments and standard streams.
func Main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(commandExitCode(err))
	}
}
func run(args []string, out io.Writer) error {
	if len(args) > 0 && (args[0] == "bootstrap" || args[0] == "auth") {
		return runIdentity(args, out)
	}
	if len(args) > 0 && args[0] == "run" {
		return runRun(args, out)
	}
	if len(args) > 0 && args[0] == "scene" {
		return runScene(args, out)
	}
	flags := flag.NewFlagSet("guardrailctl", flag.ContinueOnError)
	flags.SetOutput(out)
	flags.Usage = func() {
		fmt.Fprintln(out, "Usage: guardrailctl [--server URL] status|version|capabilities\n       guardrailctl bootstrap init --help\n       guardrailctl auth login|logout|whoami|create-key|list-keys|rotate-key|revoke-key --help\n       guardrailctl scene apply --help\n       guardrailctl run create|get|wait|revision|submission|list|submit|rotate-credential --help")
		flags.PrintDefaults()
	}
	server := flags.String("server", defaultServerURL(), "OpenGuardrail service URL")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return usageError("invalid flags")
	}
	if flags.NArg() != 1 {
		return usageError("usage: guardrailctl [--server URL] status|version|capabilities")
	}
	command := flags.Arg(0)
	if command != "status" && command != "version" && command != "capabilities" {
		return usageError("supported commands: status, version, capabilities")
	}
	token := os.Getenv("OPEN_GUARDRAIL_TOKEN")
	client, err := newAPIClient(*server, token, 5*time.Second, diagnosticResponseLimit, false)
	if err != nil {
		return err
	}
	ctx := context.Background()
	var result response
	switch command {
	case "status":
		result, err = client.generated.GetReadinessWithResponse(ctx)
	case "version":
		result, err = client.generated.GetVersionWithResponse(ctx)
	case "capabilities":
		result, err = client.generated.GetCapabilitiesWithResponse(ctx)
	}
	if err != nil {
		return transportError(err, false)
	}
	if err = validateResponse(result, diagnosticResponseLimit, command+" request", false); err != nil {
		return err
	}
	_, err = out.Write(result.GetBody())
	return err
}
