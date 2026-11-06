package ctlcmd

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	contract "github.com/alibaba/open-guardrail/api/openapi"
	"github.com/alibaba/open-guardrail/internal/adapters/security"
	runapp "github.com/alibaba/open-guardrail/internal/application/run"
	rundomain "github.com/alibaba/open-guardrail/internal/domain/run"
)

const runResponseLimit = 2 << 20

var validRunResourceID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`)

type runOptions struct {
	server, keyFile, tokenFile, scene, status, cursor, changeSetFile, submissionFile, operationID, runID, submissionID string
	limit                                                                                                              int
}

func runRun(args []string, out io.Writer) error {
	if len(args) == 2 && (args[1] == "-h" || args[1] == "--help") {
		fmt.Fprintln(out, "Usage: guardrailctl run create|get|revision|submission|list|submit|rotate-credential [flags]")
		fmt.Fprintln(out, "\nCommands:\n  create             Create an immutable Run and save its Submission Credential\n  get                Get one Run by ID\n  revision           Read the immutable Revision selected by a Run\n  submission         Read a persisted Submission within a Run\n  list               List Runs in stable descending creation order\n  submit             Submit a complete Snapshot with a Run Submission Credential\n  rotate-credential  Replace and save the Submission Credential for an OPEN Run")
		return nil
	}
	if len(args) < 2 || (args[1] != "create" && args[1] != "get" && args[1] != "revision" && args[1] != "submission" && args[1] != "list" && args[1] != "submit" && args[1] != "rotate-credential") {
		return usageError("usage: guardrailctl run create|get|revision|submission|list|submit|rotate-credential [flags]")
	}
	command := args[1]
	opts, err := parseRunOptions(command, args[2:], out)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return usageError(err.Error())
	}
	var credential credentialFile
	if command == "submit" {
		credential, err = readSubmissionCredential(opts)
	} else {
		credential, err = readProjectKey(opts)
	}
	if err != nil {
		return err
	}
	client, err := newRunClient(opts.server, credential.Token)
	if err != nil {
		return err
	}
	if command == "revision" {
		result, err := client.generated.GetRunPolicyRevisionWithResponse(context.Background(), opts.runID)
		if err != nil {
			return transportError(err, false)
		}
		if err = validateResponse(result, runResponseLimit, "Run content lookup", false); err != nil {
			return err
		}
		if result.JSON200 == nil || result.JSON200.Id == "" || (result.JSON200.Digest == nil || *result.JSON200.Digest == "") {
			return internalError("invalid Run Revision response")
		}
		_, err = out.Write(result.Body)
		return err
	}
	if command == "submission" {
		result, err := client.generated.GetRunSubmissionWithResponse(context.Background(), opts.runID, opts.submissionID)
		if err != nil {
			return transportError(err, false)
		}
		if err = validateResponse(result, runResponseLimit, "Run content lookup", false); err != nil {
			return err
		}
		if result.JSON200 == nil || result.JSON200.Id != opts.submissionID || result.JSON200.RunId != opts.runID {
			return internalError("invalid stored Submission response")
		}
		_, err = out.Write(result.Body)
		return err
	}
	if command == "get" {
		result, err := client.generated.GetPolicyRunWithResponse(context.Background(), opts.runID)
		if err != nil {
			return transportError(err, false)
		}
		if err = validateResponse(result, runResponseLimit, "Run lookup", false); err != nil {
			return err
		}
		if result.JSON200 == nil || result.JSON200.Id != opts.runID {
			return internalError("invalid Run response")
		}
		_, err = out.Write(result.Body)
		return err
	}
	if command == "list" {
		return listRuns(opts, client, out)
	}
	if command == "submit" {
		return submitRun(opts, client, out)
	}
	if command == "rotate-credential" {
		return rotateSubmissionCredential(opts, client, out)
	}
	return createRun(opts, client, out)
}

func parseRunOptions(command string, args []string, out io.Writer) (runOptions, error) {
	var opts runOptions
	flags := flag.NewFlagSet("run "+command, flag.ContinueOnError)
	flags.SetOutput(out)
	flags.StringVar(&opts.server, "server", defaultServerURL(), "service origin (HTTPS or literal loopback HTTP)")
	if command != "submit" {
		flags.StringVar(&opts.keyFile, "key-file", "", "private Project key file")
	}
	if command == "create" {
		flags.StringVar(&opts.scene, "scene", "", "Scene key")
		flags.StringVar(&opts.changeSetFile, "change-set", "", "JSON object file")
		flags.StringVar(&opts.tokenFile, "submission-credential-file", "", "new private file for the Run Submission Credential")
		flags.StringVar(&opts.operationID, "operation-id", "", "stable retry identifier; generated when omitted")
	} else if command == "submit" {
		flags.StringVar(&opts.tokenFile, "submission-credential-file", "", "private Run Submission Credential file")
		flags.StringVar(&opts.submissionFile, "submission", "", "complete Submission Snapshot JSON file")
		flags.StringVar(&opts.operationID, "operation-id", "", "stable retry identifier; generated when omitted")
	} else if command == "rotate-credential" {
		flags.StringVar(&opts.tokenFile, "submission-credential-file", "", "new private file for the replacement Run Submission Credential")
	} else if command == "list" {
		flags.StringVar(&opts.scene, "scene", "", "optional exact Scene key")
		flags.StringVar(&opts.status, "status", "", "optional Run status: OPEN, FINISHED or EXPIRED")
		flags.IntVar(&opts.limit, "limit", runapp.DefaultListLimit, "maximum Runs to return (1-100)")
		flags.StringVar(&opts.cursor, "cursor", "", "opaque cursor returned by the preceding page")
	}
	if command == "submission" {
		flags.StringVar(&opts.submissionID, "submission-id", "", "persisted Submission ID within the Run")
	}
	if err := flags.Parse(args); err != nil {
		return opts, err
	}
	if command == "get" || command == "revision" || command == "submission" || command == "submit" || command == "rotate-credential" {
		if flags.NArg() != 1 {
			return opts, usageError("run " + command + " requires exactly one Run ID")
		}
		opts.runID = flags.Arg(0)
		if !validRunResourceID.MatchString(opts.runID) {
			return opts, usageError("invalid Run ID")
		}
	} else if flags.NArg() != 0 {
		return opts, usageError("unexpected positional arguments")
	}
	if command == "submission" && !validRunResourceID.MatchString(opts.submissionID) {
		return opts, usageError("--submission-id must be a valid Submission ID")
	}
	origin, err := parseServer(opts.server)
	if err != nil || origin.Path != "" {
		return opts, usageError("server must be an origin without credentials, path, query or fragment")
	}
	opts.server = origin.String()
	if command != "submit" && opts.keyFile == "" {
		return opts, usageError("--key-file is required")
	}
	if command == "create" && (opts.scene == "" || opts.changeSetFile == "" || opts.tokenFile == "") {
		return opts, usageError("--scene, --change-set and --submission-credential-file are required")
	}
	if command == "submit" && (opts.submissionFile == "" || opts.tokenFile == "") {
		return opts, usageError("--submission and --submission-credential-file are required")
	}
	if command == "rotate-credential" && opts.tokenFile == "" {
		return opts, usageError("--submission-credential-file is required")
	}
	if command == "list" {
		if opts.limit < 1 || opts.limit > runapp.MaxListLimit {
			return opts, usageError("--limit must be between 1 and 100")
		}
		if opts.status != "" && opts.status != "OPEN" && opts.status != "FINISHED" && opts.status != "EXPIRED" {
			return opts, usageError("--status must be OPEN, FINISHED or EXPIRED")
		}
	}
	return opts, nil
}

func listRuns(opts runOptions, client *apiClient, out io.Writer) error {
	limit := contract.RunLimit(opts.limit)
	params := &contract.ListPolicyRunsParams{Limit: &limit}
	if opts.status != "" {
		status := contract.RunStatusFilter(opts.status)
		params.Status = &status
	}
	if opts.scene != "" {
		scene := contract.RunSceneFilter(opts.scene)
		params.SceneKey = &scene
	}
	if opts.cursor != "" {
		cursor := contract.RunCursor(opts.cursor)
		params.Cursor = &cursor
	}
	result, err := client.generated.ListPolicyRunsWithResponse(context.Background(), params)
	if err != nil {
		return transportError(err, false)
	}
	if err = validateResponse(result, runResponseLimit, "Run list", false); err != nil {
		return err
	}
	if result.JSON200 == nil || result.JSON200.Items == nil {
		return internalError("invalid Run list response")
	}
	_, err = out.Write(result.Body)
	return err
}

func submitRun(opts runOptions, client *apiClient, out io.Writer) error {
	payload, err := readInputFile(opts.submissionFile, rundomain.MaxSubmissionBytes)
	if err != nil {
		return usageError("submission must be a readable regular JSON file of at most 1 MiB")
	}
	if !json.Valid(payload) {
		return usageError("cannot read a valid Submission JSON file")
	}

	generatedOperationID := false
	if opts.operationID == "" {
		opts.operationID, err = newOperationID()
		if err != nil {
			return internalError("cannot generate operation ID")
		}
		generatedOperationID = true
	}
	result, err := client.generated.SubmitSnapshotWithBodyWithResponse(context.Background(), opts.runID,
		&contract.SubmitSnapshotParams{IdempotencyKey: opts.operationID}, "application/json", bytes.NewReader(payload))
	if err != nil {
		return preserveRunOperationID(transportError(err, true), generatedOperationID, opts.operationID)
	}
	if err = validateResponse(result, runResponseLimit, "Run submission", true); err != nil {
		if result.StatusCode() == 0 || result.StatusCode() >= http.StatusInternalServerError ||
			int64(len(result.GetBody())) > runResponseLimit {
			return preserveRunOperationID(err, generatedOperationID, opts.operationID)
		}
		return err
	}
	submission := result.JSON201
	if submission == nil {
		submission = result.JSON200
	}
	if submission == nil || submission.Id == "" || submission.RunId != opts.runID {
		return preserveRunOperationID(internalError("invalid Submission response"), generatedOperationID, opts.operationID)
	}
	_, err = out.Write(result.Body)
	return preserveRunOperationID(err, generatedOperationID, opts.operationID)
}

func readProjectKey(opts runOptions) (credentialFile, error) {
	data, err := security.ReadPrivateFile(opts.keyFile)
	if err != nil {
		return credentialFile{}, err
	}
	var saved credentialFile
	if json.Unmarshal(data, &saved) != nil || saved.Server != opts.server || !strings.HasPrefix(saved.Token, "ogk_") {
		return credentialFile{}, errors.New("Project key file does not match this server")
	}
	return saved, nil
}

func readSubmissionCredential(opts runOptions) (credentialFile, error) {
	data, err := security.ReadPrivateFile(opts.tokenFile)
	if err != nil {
		return credentialFile{}, err
	}
	var saved credentialFile
	if json.Unmarshal(data, &saved) != nil || saved.Server != opts.server || saved.RunID != opts.runID ||
		!strings.HasPrefix(saved.Token, "ogrt_") {
		return credentialFile{}, errors.New("Run Submission Credential file does not match this server and Run")
	}
	return saved, nil
}

func createRun(opts runOptions, client *apiClient, out io.Writer) error {
	changeSet, err := readInputFile(opts.changeSetFile, rundomain.MaxChangeSetBytes)
	if err != nil {
		return usageError("changeSet must be a readable regular JSON file of at most 256 KiB")
	}

	if !json.Valid(changeSet) {
		return usageError("changeSet must contain valid JSON")
	}
	request := contract.CreatePolicyRunJSONRequestBody{SceneKey: opts.scene, ChangeSet: json.RawMessage(changeSet)}
	generatedOperationID := false
	if opts.operationID == "" {
		opts.operationID, err = newOperationID()
		if err != nil {
			return internalError("cannot generate operation ID")
		}
		generatedOperationID = true
	}
	var created contract.PolicyRun
	err = saveNewCredential(opts.tokenFile, func() (credentialFile, error) {
		result, requestErr := client.generated.CreatePolicyRunWithResponse(context.Background(),
			&contract.CreatePolicyRunParams{IdempotencyKey: opts.operationID}, request)
		if requestErr != nil {
			return credentialFile{}, preserveRunOperationID(transportError(requestErr, true), generatedOperationID, opts.operationID)
		}
		if responseErr := validateResponse(result, runResponseLimit, "Run creation", true); responseErr != nil {
			if result.StatusCode() == 0 || result.StatusCode() >= http.StatusInternalServerError || int64(len(result.GetBody())) > runResponseLimit {
				responseErr = preserveRunOperationID(responseErr, generatedOperationID, opts.operationID)
			}
			return credentialFile{}, responseErr
		}
		if result.StatusCode() == http.StatusOK {
			return credentialFile{}, &commandError{kind: exitServerRejected, message: "Run already exists and its plaintext Submission Credential cannot be replayed; inspect any saved credential file or rotate the credential"}
		}
		if result.JSON201 == nil || result.JSON201.Run.Id == "" || result.JSON201.SubmissionCredential.Token == "" ||
			result.JSON201.SubmissionCredential.RunId != result.JSON201.Run.Id {
			return credentialFile{}, preserveRunOperationID(internalError("invalid Run creation response; do not automatically retry"), generatedOperationID, opts.operationID)
		}
		created = result.JSON201.Run
		return credentialFile{Server: opts.server, RunID: created.Id, Token: result.JSON201.SubmissionCredential.Token,
			ExpiresAt: result.JSON201.SubmissionCredential.ExpiresAt}, nil
	})
	if err != nil {
		if errors.Is(err, errCredentialOutputExists) {
			return usageError("Submission Credential file already exists; inspect it before retrying or use a new file for rotation")
		}
		return err
	}
	return json.NewEncoder(out).Encode(created)
}

func rotateSubmissionCredential(opts runOptions, client *apiClient, out io.Writer) error {
	err := saveNewCredential(opts.tokenFile, func() (credentialFile, error) {
		result, requestErr := client.generated.RotatePolicySubmissionCredentialWithResponse(context.Background(), opts.runID)
		if requestErr != nil {
			return credentialFile{}, transportError(requestErr, true)
		}
		if responseErr := validateResponse(result, runResponseLimit, "Run Submission Credential rotation", true); responseErr != nil {
			return credentialFile{}, responseErr
		}
		if result.JSON201 == nil || result.JSON201.Token == "" || result.JSON201.RunId != opts.runID {
			return credentialFile{}, internalError("invalid Submission Credential rotation response; do not automatically retry")
		}
		return credentialFile{Server: opts.server, RunID: opts.runID, Token: result.JSON201.Token,
			ExpiresAt: result.JSON201.ExpiresAt}, nil
	})
	if err != nil {
		if errors.Is(err, errCredentialOutputExists) {
			return usageError("replacement Run Submission Credential file already exists; use a new file path")
		}
		return err
	}
	_, err = fmt.Fprintln(out, "Run Submission Credential saved to the requested private file; no secret was printed.")
	return err
}

func preserveRunOperationID(err error, generated bool, operationID string) error {
	if err == nil || !generated {
		return err
	}
	message := err.Error()
	if !strings.Contains(message, "state may have changed") {
		message += "; state may have changed"
	}
	return &commandError{kind: commandExitCode(err), message: message + "; retry with --operation-id " + operationID}
}

func newOperationID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "op_" + hex.EncodeToString(value[:]), nil
}

func newRunClient(server, token string) (*apiClient, error) {
	return newCompatibleClient(server, token, runResponseLimit)
}
