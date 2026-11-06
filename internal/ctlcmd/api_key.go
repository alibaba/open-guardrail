package ctlcmd

import (
	"context"
	"fmt"
	"io"
	"time"

	contract "github.com/alibaba/open-guardrail/api/openapi"
)

const keyListResponseLimit = 128 << 10

func listMachineKeys(opts identityOptions, token string, out io.Writer) error {
	client, err := newCompatibleClient(opts.server, token, keyListResponseLimit)
	if err != nil {
		return err
	}
	params := &contract.ListProjectAPIKeysParams{PageSize: &opts.pageSize}
	if opts.after != "" {
		params.After = &opts.after
	}
	result, err := client.generated.ListProjectAPIKeysWithResponse(context.Background(), params)
	if err != nil {
		return transportError(err, false)
	}
	if err := validateResponse(result, keyListResponseLimit, "key list request", false); err != nil {
		return err
	}
	if result.JSON200 == nil {
		return internalError("invalid key list response")
	}
	_, err = out.Write(result.Body)
	return err
}

func revokeMachineKey(opts identityOptions, token string, out io.Writer) error {
	client, err := newIdentityClient(opts.server, token)
	if err != nil {
		return err
	}
	params := &contract.RevokeProjectAPIKeyParams{XGuardrailCSRF: contract.RevokeProjectAPIKeyParamsXGuardrailCSRFN1}
	result, err := client.generated.RevokeProjectAPIKeyWithResponse(context.Background(), opts.keyID, params)
	if err != nil {
		return transportError(err, true)
	}
	if err := validateResponse(result, identityResponseLimit, "key revocation request", true); err != nil {
		return err
	}
	if result.StatusCode() != 204 {
		return internalError("invalid key revocation response; check key metadata before retrying")
	}
	_, err = fmt.Fprintln(out, "Machine key revoked; remove its local credential file after updating consumers.")
	return err
}

func rotateMachineKey(opts identityOptions, token string, out io.Writer) error {
	if opts.resultFile == "" {
		return usageError("--result-file is required; use a new file path")
	}
	client, err := newIdentityClient(opts.server, token)
	if err != nil {
		return err
	}
	err = saveNewCredential(opts.resultFile, func() (credentialFile, error) {
		params := &contract.RotateProjectAPIKeyParams{XGuardrailCSRF: contract.RotateProjectAPIKeyParamsXGuardrailCSRFN1}
		request := contract.RotateAPIKeyRequest{ExpectedVersion: opts.expectedVersion}
		if opts.lifetimeSet {
			seconds := int64(opts.lifetime / time.Second)
			request.ExpiresInSeconds = &seconds
		}
		result, err := client.generated.RotateProjectAPIKeyWithResponse(context.Background(), opts.keyID, params, request)
		if err != nil {
			return credentialFile{}, transportError(err, true)
		}
		if err := validateResponse(result, identityResponseLimit, "key rotation request", true); err != nil {
			return credentialFile{}, err
		}
		if result.JSON201 == nil || result.JSON201.ApiKey == "" || result.JSON201.Id != opts.keyID || result.JSON201.Version != opts.expectedVersion+1 {
			return credentialFile{}, internalError("invalid rotation response; inspect key metadata before retrying")
		}
		return credentialFile{Server: opts.server, Token: result.JSON201.ApiKey, KeyID: result.JSON201.Id, ExpiresAt: result.JSON201.ExpiresAt}, nil
	})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "Replacement key saved to the requested private file. The old secret is no longer valid.")
	return err
}
