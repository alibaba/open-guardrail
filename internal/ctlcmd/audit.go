package ctlcmd

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	contract "github.com/alibaba/open-guardrail/api/openapi"
)

const auditResponseLimit = 4 << 20

func runAudit(args []string, out io.Writer) error {
	if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
		fmt.Fprintln(out, "Usage: guardrailctl audit list|export [flags]\nAdministrator-scoped audit queries. Export returns one redacted page.")
		return nil
	}
	if len(args) < 2 || (args[1] != "list" && args[1] != "export") {
		return usageError("usage: guardrailctl audit list|export [flags]")
	}
	flags := flag.NewFlagSet("audit "+args[1], flag.ContinueOnError)
	flags.SetOutput(out)
	server := flags.String("server", defaultServerURL(), "service origin (HTTPS or literal loopback HTTP)")
	sessionFile := flags.String("session-file", "", "private administrator CLI session file")
	event := flags.String("event-type", "", "exact event type")
	actor := flags.String("actor-id", "", "exact actor ID")
	resourceType := flags.String("resource-type", "", "exact stored resource type")
	resourceID := flags.String("resource-id", "", "exact stored resource ID")
	from := flags.String("from", "", "inclusive RFC3339 timestamp")
	to := flags.String("to", "", "exclusive RFC3339 timestamp")
	limit := flags.Int("limit", 20, "maximum events (1-100)")
	cursor := flags.String("cursor", "", "opaque cursor from the preceding page with the same filters")
	var output string
	if args[1] == "export" {
		flags.StringVar(&output, "output", "", "new private JSON file; defaults to stdout")
	}
	if err := flags.Parse(args[2:]); errors.Is(err, flag.ErrHelp) {
		return nil
	} else if err != nil {
		return usageError(err.Error())
	}
	if flags.NArg() != 0 || *limit < 1 || *limit > 100 {
		return usageError("use no positional arguments and --limit between 1 and 100")
	}
	empty := false
	flags.Visit(func(f *flag.Flag) {
		if f.Value.String() == "" {
			empty = true
		}
	})
	if empty {
		return usageError("explicit flag values cannot be empty")
	}
	origin, err := parseServer(*server)
	if err != nil || origin.Path != "" {
		return usageError("server must be an origin without credentials, path, query or fragment")
	}
	*server = origin.String()
	if *sessionFile == "" {
		path, _, err := defaultSessionFile()
		if err != nil {
			return err
		}
		*sessionFile = path
	}
	session, err := readSession(identityOptions{server: *server, sessionFile: *sessionFile})
	if err != nil {
		return err
	}
	params := &contract.ListAuditEventsParams{Limit: limit}
	if *event != "" {
		params.EventType = event
	}
	if *actor != "" {
		params.ActorId = actor
	}
	if *resourceType != "" {
		params.ResourceType = resourceType
	}
	if *resourceID != "" {
		params.ResourceId = resourceID
	}
	if *cursor != "" {
		params.Cursor = cursor
	}
	if *from != "" {
		parsed, err := parseAuditTime(*from)
		if err != nil {
			return err
		}
		params.From = &parsed
	}
	if *to != "" {
		parsed, err := parseAuditTime(*to)
		if err != nil {
			return err
		}
		params.To = &parsed
	}
	if params.From != nil && params.To != nil && *params.From >= *params.To {
		return usageError("--from must precede --to")
	}
	redacted := args[1] == "export"
	params.Redacted = &redacted
	client, err := newCompatibleClient(*server, session.Token, auditResponseLimit)
	if err != nil {
		return err
	}
	response, err := client.generated.ListAuditEventsWithResponse(context.Background(), params)
	if err != nil {
		return transportError(err, false)
	}
	if err = validateResponse(response, auditResponseLimit, "Audit query", false); err != nil {
		return err
	}
	if response.StatusCode() != 200 || response.JSON200 == nil || response.JSON200.Items == nil || response.JSON200.Redacted != redacted {
		return internalError("invalid audit page response")
	}
	return writeExport(output, response.Body, out)
}
func parseAuditTime(value string) (int64, error) {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil || parsed.Unix() < 0 {
		return 0, usageError("audit times must be RFC3339 timestamps on or after the Unix epoch")
	}
	return parsed.Unix(), nil
}
