package ctlcmd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	contract "github.com/alibaba/open-guardrail/api/openapi"
	"github.com/alibaba/open-guardrail/internal/domain/resource"
)

type bindingOptions struct {
	server, sessionFile, scene, kind, resourceID, policyID, revisionID, after string
	limit                                                                     int
}

func runBinding(args []string, out io.Writer) error {
	if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
		fmt.Fprintln(out, "Usage: guardrailctl binding list|put|delete [flags]\nManage exact resource bindings with an administrator CLI session. Put without --revision-id follows the Policy active Revision.")
		return nil
	}
	if len(args) < 2 || (args[1] != "list" && args[1] != "put" && args[1] != "delete") {
		return usageError("usage: guardrailctl binding list|put|delete [flags]")
	}
	command := args[1]
	opts, err := parseBindingOptions(command, args[2:], out)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return usageError(err.Error())
	}
	session, err := readSession(identityOptions{server: opts.server, sessionFile: opts.sessionFile})
	if err != nil {
		return err
	}
	client, err := newSceneClient(opts.server, session.Token)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if command == "list" {
		params := &contract.ListBindingsParams{Limit: &opts.limit}
		if opts.after != "" {
			params.After = &opts.after
		}
		result, err := client.generated.ListBindingsWithResponse(ctx, opts.scene, params)
		if err != nil {
			return transportError(err, false)
		}
		if err = validateResponse(result, sceneResponseLimit, "Binding listing", false); err != nil {
			return err
		}
		if result.StatusCode() != http.StatusOK || result.JSON200 == nil || result.JSON200.Items == nil {
			return internalError("invalid Binding page response")
		}
		_, err = out.Write(result.Body)
		return err
	}
	if command == "delete" {
		result, err := client.generated.DeleteBindingWithResponse(ctx, opts.scene, opts.kind, opts.resourceID, &contract.DeleteBindingParams{XGuardrailCSRF: "1"}, preserveBindingResourceID(opts))
		if err != nil {
			return transportError(err, true)
		}
		if err = validateResponse(result, sceneResponseLimit, "Binding deletion", true); err != nil {
			return err
		}
		if result.StatusCode() != http.StatusNoContent {
			return internalError("invalid Binding deletion response")
		}
		return nil
	}
	body := contract.PutBindingJSONRequestBody{PolicyId: opts.policyID}
	if opts.revisionID != "" {
		body.PolicyRevisionId = &opts.revisionID
	}
	result, err := client.generated.PutBindingWithResponse(ctx, opts.scene, opts.kind, opts.resourceID, &contract.PutBindingParams{XGuardrailCSRF: "1"}, body, preserveBindingResourceID(opts))
	if err != nil {
		return transportError(err, true)
	}
	if err = validateResponse(result, sceneResponseLimit, "Binding update", true); err != nil {
		return err
	}
	if result.StatusCode() != http.StatusOK || result.JSON200 == nil || result.JSON200.Resource.Kind != opts.kind || result.JSON200.Resource.Id != opts.resourceID || result.JSON200.PolicyId != opts.policyID || !sameOptionalString(result.JSON200.PolicyRevisionId, body.PolicyRevisionId) {
		return internalError("invalid Binding response")
	}
	return json.NewEncoder(out).Encode(result.JSON200)
}

func parseBindingOptions(command string, args []string, out io.Writer) (bindingOptions, error) {
	var opts bindingOptions
	flags := flag.NewFlagSet("binding "+command, flag.ContinueOnError)
	flags.SetOutput(out)
	flags.StringVar(&opts.server, "server", defaultServerURL(), "service origin (HTTPS or literal loopback HTTP)")
	flags.StringVar(&opts.sessionFile, "session-file", "", "private administrator CLI session file (defaults to the OS user configuration directory)")
	flags.StringVar(&opts.scene, "scene", "", "Scene key")
	if command == "list" {
		flags.IntVar(&opts.limit, "limit", 20, "maximum bindings to return (1-100)")
		flags.StringVar(&opts.after, "after", "", "opaque nextAfter cursor from the preceding page")
	} else {
		flags.StringVar(&opts.kind, "kind", "", "resource kind")
		flags.StringVar(&opts.resourceID, "resource-id", "", "exact resource ID; pass the original value without URL encoding")
	}
	if command == "put" {
		flags.StringVar(&opts.policyID, "policy-id", "", "Policy in this Scene")
		flags.StringVar(&opts.revisionID, "revision-id", "", "optional published Revision; omit to follow the Policy active Revision")
	}
	if err := flags.Parse(args); err != nil {
		return opts, err
	}
	if flags.NArg() != 0 {
		return opts, usageError("unexpected positional arguments")
	}
	explicitEmpty := false
	flags.Visit(func(f *flag.Flag) {
		if f.Value.String() == "" {
			explicitEmpty = true
		}
	})
	if explicitEmpty {
		return opts, usageError("explicit flag values cannot be empty")
	}
	if opts.sessionFile == "" {
		session, _, err := defaultSessionFile()
		if err != nil {
			return opts, err
		}
		opts.sessionFile = session
	}
	origin, err := parseServer(opts.server)
	if err != nil || origin.Path != "" {
		return opts, usageError("server must be an origin without credentials, path, query or fragment")
	}
	opts.server = origin.String()
	if opts.scene == "" {
		return opts, usageError("--scene is required")
	}
	if command == "list" {
		if opts.limit < 1 || opts.limit > 100 {
			return opts, usageError("--limit must be between 1 and 100")
		}
	} else if !(resource.Reference{Kind: opts.kind, ID: opts.resourceID}).Valid() {
		return opts, usageError("--kind and --resource-id must form a valid resource reference")
	}
	if command == "put" && (!validRunResourceID.MatchString(opts.policyID) || (opts.revisionID != "" && !validRunResourceID.MatchString(opts.revisionID))) {
		return opts, usageError("--policy-id and optional --revision-id must be valid identifiers")
	}
	return opts, nil
}

func sameOptionalString(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// The generated URL builder resolves literal dot segments. Preserve these
// valid resource IDs as encoded path segments, as required by the HTTP API.
func preserveBindingResourceID(opts bindingOptions) contract.RequestEditorFn {
	return func(_ context.Context, request *http.Request) error {
		if opts.resourceID == "." || opts.resourceID == ".." {
			prefix := "/api/v1alpha1/scenes/" + url.PathEscape(opts.scene) + "/bindings/" + url.PathEscape(opts.kind) + "/"
			request.URL.RawPath = prefix + strings.Repeat("%2E", len(opts.resourceID))
			path, err := url.PathUnescape(request.URL.RawPath)
			if err != nil {
				return err
			}
			request.URL.Path = path
		}
		return nil
	}

}
