package ctlcmd

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	contract "github.com/alibaba/open-guardrail/api/openapi"
	rundomain "github.com/alibaba/open-guardrail/internal/domain/run"
	"go.yaml.in/yaml/v3"
)

const (
	mcpFrameLimit = 16 << 20
	mcpRunIDArg   = "_guardrail_run_id"
)

type mcpProxyConfig struct {
	Version               int                      `yaml:"version"`
	Server                string                   `yaml:"server"`
	DefaultAction         string                   `yaml:"defaultAction"`
	DestructiveHintPolicy mcpDestructiveHintPolicy `yaml:"destructiveHintPolicy"`
	Rules                 []mcpProxyRule           `yaml:"rules"`
}

type mcpDestructiveHintPolicy struct {
	Scene         string `yaml:"scene"`
	ReuseValidRun *bool  `yaml:"reuseValidRun"`
}

type mcpProxyRule struct {
	Name          string        `yaml:"name"`
	Tools         []string      `yaml:"tools"`
	When          *mcpProxyWhen `yaml:"when"`
	Scene         string        `yaml:"scene"`
	ReuseValidRun bool          `yaml:"reuseValidRun"`
}

type mcpProxyWhen struct {
	Arg             string  `yaml:"arg"`
	NotReadOnlySQL  *bool   `yaml:"notReadOnlySql"`
	MatchesRegex    *string `yaml:"matchesRegex"`
	NotMatchesRegex *string `yaml:"notMatchesRegex"`
	Equals          *string `yaml:"equals"`
	NotEquals       *string `yaml:"notEquals"`
	compiled        *regexp.Regexp
}

func loadMCPProxyConfig(name string) (mcpProxyConfig, error) {
	var cfg mcpProxyConfig
	data, err := readInputFile(name, 1<<20)
	if err != nil {
		return cfg, usageError("--config must be a readable regular YAML file of at most 1 MiB")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err = decoder.Decode(&cfg); err != nil {
		return cfg, usageError("invalid MCP proxy policy: " + err.Error())
	}
	if decoder.Decode(new(any)) != io.EOF || cfg.Version != 1 {
		return cfg, usageError("MCP proxy policy requires one YAML document with version: 1")
	}
	if cfg.DefaultAction == "" {
		cfg.DefaultAction = "pass_through"
	}
	if cfg.DefaultAction != "pass_through" && cfg.DefaultAction != "deny" {
		return cfg, usageError("defaultAction must be pass_through or deny")
	}
	names := map[string]bool{}
	for i := range cfg.Rules {
		rule := &cfg.Rules[i]
		if rule.Name == "" || strings.ContainsRune(rule.Name, 0) || strings.HasPrefix(rule.Name, "destructive-hint:") || names[rule.Name] || rule.Scene == "" || strings.ContainsRune(rule.Scene, 0) || len(rule.Tools) == 0 {
			return cfg, usageError("each MCP rule requires a unique name, scene and tools; destructive-hint: is reserved")
		}
		names[rule.Name] = true
		for _, pattern := range rule.Tools {
			if _, err := path.Match(pattern, ""); err != nil || pattern == "" {
				return cfg, usageError("invalid tool pattern in rule " + rule.Name)
			}
		}
		if rule.When == nil {
			continue
		}
		when := rule.When
		count := 0
		for _, present := range []bool{when.NotReadOnlySQL != nil, when.MatchesRegex != nil, when.NotMatchesRegex != nil, when.Equals != nil, when.NotEquals != nil} {
			if present {
				count++
			}
		}
		if when.Arg == "" || count != 1 || (when.NotReadOnlySQL != nil && !*when.NotReadOnlySQL) {
			return cfg, usageError("when requires arg and exactly one predicate; notReadOnlySql must be true")
		}
		pattern := when.MatchesRegex
		if pattern == nil {
			pattern = when.NotMatchesRegex
		}
		if pattern != nil {
			when.compiled, err = regexp.Compile(*pattern)
			if err != nil {
				return cfg, usageError("invalid regex in rule " + rule.Name)
			}
		}
	}
	if strings.ContainsRune(cfg.DestructiveHintPolicy.Scene, 0) {
		return cfg, usageError("invalid destructiveHintPolicy.scene")
	}
	return cfg, nil
}

func (w *mcpProxyWhen) matches(args map[string]any) bool {
	value, ok := args[w.Arg].(string)
	negative := w.NotReadOnlySQL != nil || w.NotMatchesRegex != nil || w.NotEquals != nil
	if !ok {
		return negative
	}
	switch {
	case w.NotReadOnlySQL != nil:
		return !isStrictReadOnlySQL(value)
	case w.Equals != nil:
		return value == *w.Equals
	case w.NotEquals != nil:
		return value != *w.NotEquals
	case w.MatchesRegex != nil:
		return w.compiled.MatchString(value)
	case w.NotMatchesRegex != nil:
		return !w.compiled.MatchString(value)
	}
	return true
}

func runMCPProxy(args []string, in io.Reader, out, diagnostic io.Writer) error {
	flags := flag.NewFlagSet("mcp-proxy", flag.ContinueOnError)
	flags.SetOutput(diagnostic)
	flags.Usage = func() {
		fmt.Fprintln(diagnostic, "Usage: guardrailctl mcp-proxy --config POLICY.yaml [--server URL] [--run-dir DIR] -- <downstream-cmd> [args...]")
		flags.PrintDefaults()
	}
	configName := flags.String("config", "", "MCP proxy policy YAML")
	server := flags.String("server", "", "OpenGuardrail service origin (overrides policy)")
	runDir := flags.String("run-dir", "", "private credential directory (default: temporary, removed on exit)")
	separator := -1
	for i, arg := range args {
		if arg == "--" {
			separator = i
			break
		}
	}
	flagArgs := args
	if separator >= 0 {
		flagArgs = args[:separator]
	}
	if err := flags.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return usageError("invalid mcp-proxy flags")
	}
	if *configName == "" || separator < 0 || separator+1 == len(args) || flags.NArg() != 0 {
		return usageError("mcp-proxy requires --config POLICY.yaml and -- <downstream-cmd> [args...]")
	}
	cfg, err := loadMCPProxyConfig(*configName)
	if err != nil {
		return err
	}
	if *server != "" {
		cfg.Server = *server
	}
	if cfg.Server == "" {
		cfg.Server = defaultServerURL()
	}
	origin, err := parseServer(cfg.Server)
	if err != nil || origin.Path != "" {
		return usageError("server must be an origin without credentials, path, query or fragment")
	}
	cfg.Server = origin.String()
	dir, cleanup, err := prepareMCPRunDir(*runDir)
	if err != nil {
		return err
	}
	defer cleanup()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	command := args[separator+1:]
	child := exec.CommandContext(ctx, command[0], command[1:]...)
	child.Stderr = diagnostic
	child.WaitDelay = time.Second
	childIn, err := child.StdinPipe()
	if err != nil {
		return internalError("cannot open downstream stdin")
	}
	defer childIn.Close()
	childOut, err := child.StdoutPipe()
	if err != nil {
		return internalError("cannot open downstream stdout")
	}
	if err = child.Start(); err != nil {
		return internalError("cannot start downstream MCP server: " + err.Error())
	}
	proxy := newMCPProxy(cfg, dir)
	err = proxy.relay(ctx, in, out, childIn, childOut, diagnostic)
	waitDone := make(chan error, 1)
	go func() { waitDone <- child.Wait() }()
	var waitErr error
	select {
	case waitErr = <-waitDone:
	case <-time.After(time.Second):
		cancel()
		waitErr = <-waitDone
	}
	if err != nil {
		return err
	}
	// Allow a clean EOF to finish before canceling the command. Canceling first
	// can make exec.Wait report context.Canceled even after a successful exit.
	if waitErr != nil && ctx.Err() == nil {
		return internalError("downstream MCP server exited unsuccessfully")
	}
	return nil
}

func prepareMCPRunDir(dir string) (string, func(), error) {
	if dir == "" {
		name, err := os.MkdirTemp("", "og-mcp-proxy-")
		if err != nil {
			return "", nil, internalError("cannot create private MCP credential directory")
		}
		return name, func() { _ = os.RemoveAll(name) }, nil
	}
	name, err := filepath.Abs(dir)
	if err == nil {
		err = os.MkdirAll(name, 0700)
	}
	if err != nil {
		return "", nil, usageError("cannot create --run-dir")
	}
	info, err := os.Lstat(name)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return "", nil, usageError("--run-dir must be a private directory (0700), not a symlink")
	}
	return name, func() {}, nil
}

type mcpRunBinding struct {
	Source             string         `json:"source"`
	Rule               string         `json:"rule"`
	Tool               string         `json:"tool"`
	ArgumentsDigest    string         `json:"argumentsDigest"`
	Arguments          map[string]any `json:"arguments,omitempty"`
	ArgumentsTruncated bool           `json:"argumentsTruncated,omitempty"`
}

type mcpAllowedRun struct {
	runID     string
	expiresAt int64
}

type mcpProxy struct {
	config           mcpProxyConfig
	runDir           string
	client           *apiClient // Lazily negotiated: pass-through never makes an HTTP request.
	issued           map[string]string
	allows           map[string]mcpAllowedRun
	mu               sync.RWMutex
	pendingToolsList map[string]struct{}
	destructiveTools map[string]struct{}
}

func newMCPProxy(cfg mcpProxyConfig, dir string) *mcpProxy {
	return &mcpProxy{config: cfg, runDir: dir, issued: map[string]string{}, allows: map[string]mcpAllowedRun{},
		pendingToolsList: map[string]struct{}{}, destructiveTools: map[string]struct{}{}}
}

func canonicalMCPArgs(tool string, args map[string]any) (map[string]any, string, error) {
	clean := make(map[string]any, len(args))
	for key, value := range args {
		if key != mcpRunIDArg {
			clean[key] = value
		}
	}
	data, err := json.Marshal(struct {
		Tool      string         `json:"tool"`
		Arguments map[string]any `json:"arguments"`
	}{tool, clean})
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(data)
	return clean, "sha256:" + hex.EncodeToString(digest[:]), nil
}

func mcpCacheKey(rule mcpProxyRule, digest string) string {
	return rule.Name + "\x00" + rule.Scene + "\x00" + digest
}

func (p *mcpProxy) match(tool string, args map[string]any) (*mcpProxyRule, string) {
	for _, rule := range p.config.Rules {
		for _, pattern := range rule.Tools {
			matched, _ := path.Match(pattern, tool)
			if matched && (rule.When == nil || rule.When.matches(args)) {
				return &rule, ""
			}
		}
	}
	p.mu.RLock()
	_, destructive := p.destructiveTools[tool]
	p.mu.RUnlock()
	if destructive {
		policy := p.config.DestructiveHintPolicy
		if policy.Scene == "" {
			return nil, "tool is marked destructiveHint: true; configure destructiveHintPolicy.scene or an explicit rule"
		}
		return &mcpProxyRule{Name: "destructive-hint:" + tool, Scene: policy.Scene,
			ReuseValidRun: policy.ReuseValidRun == nil || *policy.ReuseValidRun}, ""
	}
	if p.config.DefaultAction == "deny" {
		return nil, "tool is denied by defaultAction; configure an explicit rule"
	}
	return nil, ""
}

func (p *mcpProxy) gate(ctx context.Context, rule mcpProxyRule, tool string, args map[string]any, digest, runID string) string {
	key := mcpCacheKey(rule, digest)
	if runID == "" && rule.ReuseValidRun {
		if cached, ok := p.allows[key]; ok {
			if time.Now().Unix() < cached.expiresAt {
				return ""
			}
			delete(p.allows, key)
		}
	}
	if runID != "" && p.issued[runID] != key {
		return "Run was not created by this proxy for this rule, scene and exact tool arguments; retry without _guardrail_run_id to create a matching Run"
	}
	if p.client == nil {
		client, err := newRunClient(p.config.Server, "")
		if err != nil {
			return err.Error()
		}
		p.client = client
	}
	if runID == "" {
		return p.createGateRun(ctx, rule, tool, args, digest, key)
	}
	result, err := p.client.generated.GetPolicyRunWithResponse(ctx, runID)
	if err != nil {
		return transportError(err, true).Error()
	}
	if err = validateResponse(result, runResponseLimit, "Run lookup", true); err != nil {
		return err.Error()
	}
	run := result.JSON200
	if run == nil || run.Id != runID || run.SceneKey != rule.Scene {
		return "Run identity or scene does not match this tool call"
	}
	var binding mcpRunBinding
	if json.Unmarshal(run.ChangeSet, &binding) != nil || binding.Source != "mcp-proxy" || binding.Rule != rule.Name || binding.Tool != tool || binding.ArgumentsDigest != digest {
		return "Run changeSet does not match this rule and exact tool arguments"
	}
	if run.Status != contract.PolicyRunStatusFINISHED || run.Result == nil || *run.Result != contract.PASS || run.Decision == nil || *run.Decision != contract.ALLOW || run.DecisionExpiresAt == nil || *run.DecisionExpiresAt <= time.Now().Unix() {
		return "Run requires FINISHED / PASS / ALLOW with a future decisionExpiresAt; inspect the Run and submit evidence before retrying"
	}
	if rule.ReuseValidRun {
		p.allows[key] = mcpAllowedRun{runID: runID, expiresAt: *run.DecisionExpiresAt}
	}
	return ""
}

func (p *mcpProxy) createGateRun(ctx context.Context, rule mcpProxyRule, tool string, args map[string]any, digest, key string) string {
	binding := mcpRunBinding{Source: "mcp-proxy", Rule: rule.Name, Tool: tool, ArgumentsDigest: digest, Arguments: args}
	changeSet, err := json.Marshal(binding)
	if err != nil {
		return "cannot encode tool arguments"
	}
	if len(changeSet) > rundomain.MaxChangeSetBytes {
		binding.Arguments, binding.ArgumentsTruncated = nil, true
		changeSet, err = json.Marshal(binding)
	}
	if err != nil || len(changeSet) > rundomain.MaxChangeSetBytes {
		return "tool identity exceeds the changeSet size limit"
	}
	operationID, err := newOperationID()
	if err != nil {
		return "cannot generate Run operation ID"
	}
	credentialPath := filepath.Join(p.runDir, operationID+".json")
	var runID string
	err = saveNewCredential(credentialPath, func() (credentialFile, error) {
		result, err := p.client.generated.CreatePolicyRunWithResponse(ctx,
			&contract.CreatePolicyRunParams{IdempotencyKey: operationID},
			contract.CreatePolicyRunJSONRequestBody{SceneKey: rule.Scene, ChangeSet: changeSet})
		if err != nil {
			return credentialFile{}, transportError(err, true)
		}
		if err = validateResponse(result, runResponseLimit, "Run creation", true); err != nil {
			return credentialFile{}, err
		}
		created := result.JSON201
		if created == nil || !validRunResourceID.MatchString(created.Run.Id) || created.Run.SceneKey != rule.Scene || created.Run.Status != contract.PolicyRunStatusOPEN || !sameMCPBinding(created.Run.ChangeSet, binding) || !strings.HasPrefix(created.SubmissionCredential.Token, "ogrt_") || created.SubmissionCredential.RunId != created.Run.Id {
			return credentialFile{}, internalError("invalid Run creation response; do not automatically retry")
		}
		runID = created.Run.Id
		return credentialFile{Server: p.config.Server, RunID: runID, Token: created.SubmissionCredential.Token, ExpiresAt: created.SubmissionCredential.ExpiresAt}, nil
	})
	if err != nil {
		return fmt.Sprintf("%s; Run creation operation ID: %s. Inspect server state before retrying the original tool call without _guardrail_run_id; this proxy cannot adopt Runs created by a separate CLI.", err, operationID)
	}
	p.issued[runID] = key
	return fmt.Sprintf("OpenGuardrail blocked tool %q (rule: %s, scene: %s).\nRun created: %s\nSubmission credential file (0600): %s\n\nNext steps:\n1. Inspect frozen policy: guardrailctl run revision --server %s %s\n2. Collect required evidence and submit: guardrailctl run submit --server %s --submission-credential-file %s --submission <submission.json> %s\n3. Retry this exact tool call with the same arguments plus %q: %q.",
		tool, rule.Name, rule.Scene, runID, credentialPath, mcpShellQuote(p.config.Server), mcpShellQuote(runID), mcpShellQuote(p.config.Server), mcpShellQuote(credentialPath), mcpShellQuote(runID), mcpRunIDArg, runID)
}

func sameMCPBinding(data []byte, want mcpRunBinding) bool {
	var got mcpRunBinding
	return json.Unmarshal(data, &got) == nil && got.Source == want.Source && got.Rule == want.Rule && got.Tool == want.Tool && got.ArgumentsDigest == want.ArgumentsDigest
}

func mcpShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

type mcpMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	Result  json.RawMessage `json:"result"`
}

func decodeMCPMessage(frame []byte) (mcpMessage, error) {
	var message mcpMessage
	var fields map[string]json.RawMessage
	if !utf8.Valid(frame) || json.Unmarshal(frame, &fields) != nil || fields == nil {
		return message, errors.New("invalid JSON-RPC frame")
	}
	// JSON-RPC field names are case sensitive. Decoding directly into a Go
	// struct would let an extra "Method" override the real "method" field.
	for _, field := range []struct {
		name   string
		target any
	}{
		{"jsonrpc", &message.JSONRPC}, {"method", &message.Method},
	} {
		if raw, ok := fields[field.name]; ok && json.Unmarshal(raw, field.target) != nil {
			return message, errors.New("invalid JSON-RPC field")
		}
	}
	message.ID, message.Params, message.Result = fields["id"], fields["params"], fields["result"]
	if message.JSONRPC != "2.0" {
		return message, errors.New("invalid JSON-RPC version")
	}
	return message, nil
}

// Reject ambiguous duplicate keys before forwarding a client frame. Different
// JSON parsers may otherwise assess and execute different tool names/arguments.
func mcpUniqueJSON(decoder *json.Decoder, depth int) error {
	if depth > 128 {
		return errors.New("MCP JSON nesting exceeds 128 levels")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	keys := map[string]struct{}{}
	for decoder.More() {
		if delim == '{' {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("invalid MCP JSON object key")
			}
			if _, exists := keys[key]; exists {
				return errors.New("duplicate MCP JSON object key")
			}
			keys[key] = struct{}{}
		}
		if err := mcpUniqueJSON(decoder, depth+1); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func mcpRequestID(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return ""
	}
	switch value := value.(type) {
	case string:
		data, _ := json.Marshal(value)
		return string(data)
	case json.Number:
		return string(value)
	}
	return ""
}

func (p *mcpProxy) fromClient(ctx context.Context, frame []byte) (forward, reply []byte, err error) {
	message, err := decodeMCPMessage(frame)
	if err != nil {
		return nil, nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(frame))
	decoder.UseNumber()
	if err := mcpUniqueJSON(decoder, 0); err != nil {
		return nil, nil, err
	}
	if message.Method == "tools/list" {
		if id := mcpRequestID(message.ID); id != "" {
			p.mu.Lock()
			p.pendingToolsList[id] = struct{}{}
			p.mu.Unlock()
		}
	}
	if message.Method != "tools/call" {
		return frame, nil, nil
	}
	block := func(reason string) ([]byte, []byte, error) {
		if mcpRequestID(message.ID) == "" {
			return nil, nil, errors.New("tools/call requires a request ID")
		}
		body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": message.ID,
			"result": map[string]any{"isError": true, "content": []map[string]string{{"type": "text", "text": reason}}}})
		return nil, body, err
	}
	if mcpRequestID(message.ID) == "" {
		return block("tools/call requires a request ID")
	}
	var params map[string]json.RawMessage
	var tool string
	if json.Unmarshal(message.Params, &params) != nil || json.Unmarshal(params["name"], &tool) != nil || tool == "" {
		return block("tools/call requires a tool name and object parameters")
	}
	var args map[string]any
	if data := params["arguments"]; len(data) != 0 {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber() // Preserve large integers in hashes and forwarded arguments.
		if decoder.Decode(&args) != nil {
			return block("tool arguments must be a JSON object")
		}
	}
	runID := ""
	_, hasRunID := args[mcpRunIDArg]
	if hasRunID {
		var ok bool
		runID, ok = args[mcpRunIDArg].(string)
		if !ok || !validRunResourceID.MatchString(runID) {
			return block("_guardrail_run_id must be a valid nonempty Run ID")
		}
	}
	clean, digest, err := canonicalMCPArgs(tool, args)
	if err != nil {
		return block("cannot canonicalize tool arguments")
	}
	rule, reason := p.match(tool, clean)
	if reason == "" && rule != nil {
		reason = p.gate(ctx, *rule, tool, clean, digest, runID)
	}
	if reason != "" {
		return block(reason)
	}
	if !hasRunID {
		return frame, nil, nil
	}
	// Rewrite only the reserved argument; preserve extension fields and metadata.
	params["arguments"], err = json.Marshal(clean)
	if err != nil {
		return nil, nil, err
	}
	var envelope map[string]json.RawMessage
	_ = json.Unmarshal(frame, &envelope)
	envelope["params"], _ = json.Marshal(params)
	forward, err = json.Marshal(envelope)
	return forward, nil, err
}

func (p *mcpProxy) observeDownstream(frame []byte) error {
	message, err := decodeMCPMessage(frame)
	if err != nil {
		return err
	}
	if message.Method != "" {
		return nil // Server requests may reuse IDs from the opposite direction.
	}
	id := mcpRequestID(message.ID)
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.pendingToolsList[id]; !ok {
		return nil
	}
	delete(p.pendingToolsList, id)
	var result struct {
		Tools []struct {
			Name        string `json:"name"`
			Annotations struct {
				DestructiveHint bool `json:"destructiveHint"`
			} `json:"annotations"`
		} `json:"tools"`
	}
	if json.Unmarshal(message.Result, &result) == nil {
		for _, tool := range result.Tools {
			if tool.Name != "" && tool.Annotations.DestructiveHint {
				p.destructiveTools[tool.Name] = struct{}{}
			}
		}
	}
	return nil
}

type mcpFrameWriter struct {
	mu  sync.Mutex
	out io.Writer
}

func (w *mcpFrameWriter) write(frame []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err := w.out.Write(append(append([]byte(nil), frame...), '\n'))
	return err
}

func readMCPFrame(reader *bufio.Reader) ([]byte, error) {
	var frame []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(frame)+len(part) > mcpFrameLimit+1 {
			return nil, errors.New("MCP frame exceeds 16 MiB")
		}
		frame = append(frame, part...)
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil && !(errors.Is(err, io.EOF) && len(frame) > 0) {
			return nil, err
		}
		frame = bytes.TrimSuffix(frame, []byte("\n"))
		if len(frame) > mcpFrameLimit {
			return nil, errors.New("MCP frame exceeds 16 MiB")
		}
		return frame, nil
	}
}

func (p *mcpProxy) relay(ctx context.Context, clientIn io.Reader, clientOut io.Writer, downstreamIn io.WriteCloser, downstreamOut io.Reader, diagnostic io.Writer) error {
	clientWriter, downstreamWriter := &mcpFrameWriter{out: clientOut}, &mcpFrameWriter{out: downstreamIn}
	upstreamDone, downstreamDone := make(chan error, 1), make(chan error, 1)
	go func() {
		reader := bufio.NewReader(clientIn)
		for {
			frame, err := readMCPFrame(reader)
			if err != nil {
				_ = downstreamIn.Close()
				upstreamDone <- err
				return
			}
			if len(bytes.TrimSpace(frame)) == 0 {
				continue
			}
			forward, reply, err := p.fromClient(ctx, frame)
			if err == nil && reply != nil {
				err = clientWriter.write(reply)
			}
			if err == nil && forward != nil {
				err = downstreamWriter.write(forward)
			}
			if err != nil {
				upstreamDone <- err
				return
			}
		}
	}()
	go func() {
		reader := bufio.NewReader(downstreamOut)
		for {
			frame, err := readMCPFrame(reader)
			if err == nil && len(bytes.TrimSpace(frame)) == 0 {
				continue
			}
			if err == nil {
				err = p.observeDownstream(frame)
			}
			if err == nil {
				err = clientWriter.write(frame)
			}
			if err != nil {
				downstreamDone <- err
				return
			}
		}
	}()
	var err error
	select {
	case err = <-upstreamDone:
		if errors.Is(err, io.EOF) {
			// Bound shutdown while allowing a final response to drain after stdin EOF.
			select {
			case err = <-downstreamDone:
			case <-ctx.Done():
				err = ctx.Err()
			case <-time.After(time.Second):
				err = nil
			}
		}
	case err = <-downstreamDone:
	case <-ctx.Done():
		err = ctx.Err()
	}
	_ = downstreamIn.Close()
	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return nil
	}
	if err != nil {
		fmt.Fprintln(diagnostic, "MCP proxy:", err)
	}
	return err
}
