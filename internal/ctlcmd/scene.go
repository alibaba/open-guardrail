package ctlcmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"unicode/utf8"

	contract "github.com/alibaba/open-guardrail/api/openapi"
	specdomain "github.com/alibaba/open-guardrail/internal/domain/spec"
)

const sceneResponseLimit = 512 * 1024

type sceneOptions struct {
	server, sessionFile, key, name, specFile string
	publish                                  bool
}

type sceneApplyResult struct {
	Scene        contract.SceneModel      `json:"scene"`
	Created      bool                     `json:"created"`
	DraftChanged bool                     `json:"draftChanged"`
	Revision     *contract.PolicyRevision `json:"revision,omitempty"`
}

func runScene(args []string, out io.Writer) error {
	if len(args) == 2 && (args[1] == "-h" || args[1] == "--help") {
		fmt.Fprintln(out, "Usage: guardrailctl scene apply [flags]")
		fmt.Fprintln(out, "\nCommands:\n  apply  Create a Scene if needed, save its Spec draft and optionally publish it")
		return nil
	}
	if len(args) < 2 || args[1] != "apply" {
		return usageError("usage: guardrailctl scene apply [flags]")
	}
	opts, err := parseSceneOptions(args[2:], out)
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
	return applyScene(opts, client, out)
}

func parseSceneOptions(args []string, out io.Writer) (sceneOptions, error) {
	var opts sceneOptions
	flags := flag.NewFlagSet("scene apply", flag.ContinueOnError)
	flags.SetOutput(out)
	flags.StringVar(&opts.server, "server", defaultServerURL(), "service origin (HTTPS or literal loopback HTTP)")
	flags.StringVar(&opts.sessionFile, "session-file", "", "private administrator CLI session file (defaults to the OS user configuration directory)")
	flags.StringVar(&opts.key, "key", "", "stable Scene key")
	flags.StringVar(&opts.name, "name", "", "Scene display name")
	flags.StringVar(&opts.specFile, "spec", "", "UTF-8 Spec YAML file")
	flags.BoolVar(&opts.publish, "publish", false, "publish and activate the saved draft")
	if err := flags.Parse(args); err != nil {
		return opts, err
	}
	sessionFileProvided := false
	flags.Visit(func(current *flag.Flag) {
		if current.Name == "session-file" {
			sessionFileProvided = true
		}
	})
	if opts.sessionFile == "" {
		if sessionFileProvided {
			return opts, usageError("--session-file cannot be empty")
		}
		defaultSession, _, err := defaultSessionFile()
		if err != nil {
			return opts, err
		}
		opts.sessionFile = defaultSession
	}
	if flags.NArg() != 0 {
		return opts, usageError("unexpected positional arguments")
	}
	origin, err := parseServer(opts.server)
	if err != nil || origin.Path != "" {
		return opts, usageError("server must be an origin without credentials, path, query or fragment")
	}
	opts.server = origin.String()
	if opts.key == "" || opts.name == "" || opts.specFile == "" {
		return opts, usageError("--key, --name and --spec are required")
	}
	return opts, nil
}

func applyScene(opts sceneOptions, client *apiClient, out io.Writer) error {
	yaml, err := readSpecFile(opts.specFile)
	if err != nil {
		return err
	}
	ctx := context.Background()
	result := sceneApplyResult{}
	lookup, err := client.generated.GetSceneModelWithResponse(ctx, opts.key)
	if err != nil {
		return transportError(err, false)
	}
	if lookup.StatusCode() == 404 {
		if !opts.publish {
			return usageError("new Scenes require --publish because initialization publishes and activates Revision 1 atomically")
		}
		operationID := "cli-init-" + modelDigestForCLI(opts.key+"\n"+opts.name+"\n"+yaml)
		created, err := client.generated.InitializeSceneWithResponse(ctx, opts.key, &contract.InitializeSceneParams{
			XGuardrailCSRF: "1",
			IdempotencyKey: operationID,
		}, contract.InitializeSceneJSONRequestBody{Name: opts.name, DefaultPolicyName: "default", Yaml: yaml})
		if err != nil {
			return transportError(err, true)
		}
		if err = validateResponse(created, sceneResponseLimit, "Scene initialization", true); err != nil {
			return err
		}
		if created.JSON201 == nil || created.JSON201.Scene.Key != opts.key || created.JSON201.Revision.Id == "" {
			return internalError("invalid Scene initialization response")
		}
		result.Scene = created.JSON201.Scene
		result.Created = true
		result.Revision = &created.JSON201.Revision
		return json.NewEncoder(out).Encode(result)
	}
	if err = validateResponse(lookup, sceneResponseLimit, "Scene lookup", false); err != nil {
		return err
	}
	if lookup.JSON200 == nil || lookup.JSON200.Key != opts.key {
		return internalError("invalid Scene response")
	}
	result.Scene = *lookup.JSON200
	if result.Scene.Name != opts.name {
		return &commandError{kind: exitServerRejected, message: fmt.Sprintf("Scene %q already exists with name %q", opts.key, result.Scene.Name)}
	}
	if result.Scene.DefaultPolicyId == nil {
		return &commandError{kind: exitServerRejected, message: "Scene has no default Policy; initialize it before applying a Spec"}
	}
	policyID := *result.Scene.DefaultPolicyId
	policyResponse, err := client.generated.GetPolicyWithResponse(ctx, policyID)
	if err != nil {
		return transportError(err, false)
	}
	if err = validateResponse(policyResponse, sceneResponseLimit, "Policy lookup", false); err != nil {
		return err
	}
	if policyResponse.JSON200 == nil || policyResponse.JSON200.Id != policyID {
		return internalError("invalid Policy response")
	}
	policy := *policyResponse.JSON200
	if opts.publish && policy.ActiveRevisionId != nil {
		active, lookupErr := client.generated.GetPolicyRevisionWithResponse(ctx, *policy.ActiveRevisionId)
		if lookupErr != nil {
			return transportError(lookupErr, false)
		}
		if err = validateResponse(active, sceneResponseLimit, "Revision lookup", false); err != nil {
			return err
		}
		if active.JSON200 == nil || active.JSON200.Id != *policy.ActiveRevisionId || active.JSON200.PolicyId != policyID || active.JSON200.Status != "PUBLISHED" {
			return internalError("invalid active Revision response")
		}
		if active.JSON200.Yaml == yaml {
			result.Revision = active.JSON200
			return json.NewEncoder(out).Encode(result)
		}
	}
	draftResponse, err := client.generated.GetPolicyDraftWithResponse(ctx, policyID)
	if err != nil {
		return transportError(err, false)
	}
	saveDraft := draftResponse.StatusCode() == http.StatusNotFound
	if !saveDraft {
		if err = validateResponse(draftResponse, sceneResponseLimit, "Draft Revision lookup", false); err != nil {
			return err
		}
		if draftResponse.JSON200 == nil || draftResponse.JSON200.PolicyId != policyID || draftResponse.JSON200.Status != "DRAFT" {
			return internalError("invalid Draft Revision response")
		}
		saveDraft = draftResponse.JSON200.Yaml != yaml
	}
	if saveDraft {
		saved, err := client.generated.SavePolicyDraftWithResponse(ctx, policyID, &contract.SavePolicyDraftParams{XGuardrailCSRF: "1"}, contract.SavePolicyDraftJSONRequestBody{Yaml: yaml})
		if err != nil {
			return transportError(err, true)
		}
		if err = validateResponse(saved, sceneResponseLimit, "Draft Revision save", true); err != nil {
			return err
		}
		if saved.JSON200 == nil || saved.JSON200.Id == "" || saved.JSON200.PolicyId != policyID || saved.JSON200.Status != "DRAFT" || saved.JSON200.Yaml != yaml {
			return internalError("invalid Draft Revision response")
		}
		result.DraftChanged = true
	}
	if opts.publish {
		operationID, err := newOperationID()
		if err != nil {
			return internalError("cannot generate operation ID")
		}
		published, err := client.generated.PublishPolicyDraftWithResponse(ctx, policyID,
			&contract.PublishPolicyDraftParams{XGuardrailCSRF: "1", IdempotencyKey: operationID},
			contract.PublishPolicyDraftJSONRequestBody{Yaml: yaml})
		if err != nil {
			return scenePublishError(transportError(err, true), result.DraftChanged)
		}
		if err = validateResponse(published, sceneResponseLimit, "Revision publication", true); err != nil {
			return scenePublishError(err, result.DraftChanged)
		}
		if published.JSON201 == nil || published.JSON201.Id == "" || published.JSON201.PolicyId != policyID {
			return scenePublishError(internalError("invalid Revision response"), result.DraftChanged)
		}
		revision := *published.JSON201
		activated, err := client.generated.ActivatePolicyRevisionWithResponse(ctx, policyID,
			&contract.ActivatePolicyRevisionParams{XGuardrailCSRF: "1"},
			contract.ActivatePolicyRevisionJSONRequestBody{
				PolicyRevisionId:         revision.Id,
				ExpectedActiveRevisionId: policy.ActiveRevisionId,
			})
		if err != nil {
			return scenePublishError(transportError(err, true), result.DraftChanged)
		}
		if err = validateResponse(activated, sceneResponseLimit, "Revision activation", true); err != nil {
			return scenePublishError(err, result.DraftChanged)
		}
		if activated.JSON200 == nil || activated.JSON200.Id != policyID || activated.JSON200.ActiveRevisionId == nil ||
			*activated.JSON200.ActiveRevisionId != revision.Id {
			return scenePublishError(internalError("invalid Revision activation response"), result.DraftChanged)
		}
		result.Revision = &revision
	}
	return json.NewEncoder(out).Encode(result)
}

func modelDigestForCLI(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func scenePublishError(err error, draftChanged bool) error {
	if !draftChanged {
		return err
	}
	return &commandError{kind: commandExitCode(err), message: "Spec draft was saved, but publication did not complete: " + err.Error() + "; re-run the same command after checking the active version"}
}

func readSpecFile(path string) (string, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", usageError("--spec file does not exist")
	}
	if err != nil {
		return "", usageError("cannot open --spec file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", usageError("cannot inspect --spec file")
	}
	if !info.Mode().IsRegular() {
		return "", usageError("--spec must be a regular file")
	}
	contents, err := io.ReadAll(io.LimitReader(file, specdomain.MaxYAMLBytes+1))
	if err != nil {
		return "", usageError("cannot read --spec file")
	}
	if len(contents) > specdomain.MaxYAMLBytes {
		return "", usageError("--spec exceeds the 64 KiB limit")
	}
	if !utf8.Valid(contents) {
		return "", usageError("--spec must contain valid UTF-8")
	}
	return string(contents), nil
}

func newSceneClient(server, token string) (*apiClient, error) {
	return newCompatibleClient(server, token, sceneResponseLimit)
}
