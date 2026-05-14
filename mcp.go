package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

const mcpProtocolVersion = "2024-11-05"

// ── JSON-RPC 2.0 wire types ────────────────────────────────────────────────────

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	errParseError     = -32700
	errInvalidRequest = -32600
	errMethodNotFound = -32601
	errInvalidParams  = -32602
	errInternalError  = -32603
)

// ── MCP payload types ──────────────────────────────────────────────────────────

type initializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    serverCapabilities `json:"capabilities"`
	ServerInfo      serverInfo         `json:"serverInfo"`
	Instructions    string             `json:"instructions,omitempty"`
}

type serverCapabilities struct {
	Tools *toolsCapability `json:"tools,omitempty"`
}

type toolsCapability struct {
	ListChanged bool `json:"listChanged"`
}

type serverInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type toolDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

type toolsListResult struct {
	Tools []toolDef `json:"tools"`
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type toolCallResult struct {
	Content []contentItem `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ── Tool argument types ────────────────────────────────────────────────────────

type searchDocsArgs struct {
	Query string  `json:"query"`
	TopK  float64 `json:"top_k"`
}

type checkCodeArgs struct {
	Code string `json:"code"`
}

type getRuleArgs struct {
	Code string `json:"code"`
}

type getExamplesArgs struct {
	Query string  `json:"query"`
	TopK  float64 `json:"top_k"`
}

type searchQueriesArgs struct {
	Query        string  `json:"query"`
	ResultTag    string  `json:"result_tag"`
	TechniqueTag string  `json:"technique_tag"`
	TopK         float64 `json:"top_k"`
	ListTags     bool    `json:"list_tags"`
}

// ── Tool argument types (query + forms) ───────────────────────────────────────

type generateQueryArgs struct {
	Question string  `json:"question"`
	MaxTokens float64 `json:"max_tokens"`
}

type validateQueryArgs struct {
	Query string `json:"query"`
}

type getFormInfoArgs struct {
	Name  string  `json:"name"`
	TopK  float64 `json:"top_k"`
}

// ── Server ─────────────────────────────────────────────────────────────────────

// MCPServer is the Storm MCP stdio server.
type MCPServer struct {
	store    *VectorStore
	emb      *Embedder
	clf      *Classifier
	corpus   *CorpusSearcher
	tagged   *TaggedCorpusSearcher
	queryGen *StormInfer   // nil when not embedded
	forms    *FormsIndex
	reader   *bufio.Reader
	writer   io.Writer
	log      *slog.Logger
}

func NewMCPServer(
	store *VectorStore,
	emb *Embedder,
	clf *Classifier,
	corpus *CorpusSearcher,
	tagged *TaggedCorpusSearcher,
	queryGen *StormInfer,
	forms *FormsIndex,
	r io.Reader,
	w io.Writer,
	log *slog.Logger,
) *MCPServer {
	return &MCPServer{
		store:    store,
		emb:      emb,
		clf:      clf,
		corpus:   corpus,
		tagged:   tagged,
		queryGen: queryGen,
		forms:    forms,
		reader:   bufio.NewReaderSize(r, 1<<20),
		writer:   w,
		log:      log,
	}
}

func (s *MCPServer) Run() error {
	for {
		raw, err := s.readMessage()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read message: %w", err)
		}

		var req jsonRPCRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			s.sendError(nil, errParseError, "parse error")
			continue
		}

		s.log.Info("recv", "method", req.Method, "id", string(req.ID))

		if len(req.ID) == 0 {
			s.handleNotification(req.Method)
			continue
		}

		resp := s.dispatch(req)
		if err := s.sendResponse(resp); err != nil {
			return fmt.Errorf("send response: %w", err)
		}
	}
}

func (s *MCPServer) dispatch(req jsonRPCRequest) jsonRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	case "ping":
		return jsonRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	default:
		return errResponse(req.ID, errMethodNotFound, "method not found: "+req.Method)
	}
}

func (s *MCPServer) handleNotification(method string) {
	switch method {
	case "notifications/initialized":
		s.log.Info("client initialized")
	default:
		s.log.Debug("unhandled notification", "method", method)
	}
}

func (s *MCPServer) handleInitialize(req jsonRPCRequest) jsonRPCResponse {
	genAvail := s.queryGen != nil
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: initializeResult{
			ProtocolVersion: mcpProtocolVersion,
			Capabilities:    serverCapabilities{Tools: &toolsCapability{ListChanged: false}},
			ServerInfo:      serverInfo{Name: "storm-mcp", Version: "2.0.0"},
			Instructions: fmt.Sprintf(
				"Storm language assistant for Vertex Synapse. "+
					"Indexed %d Synapse doc chunks, %d clean Storm examples, %d data model forms. "+
					"Query generation: %v. "+
					"Tools: search_synapse_docs, check_storm_code, get_rule_info, get_storm_examples, "+
					"search_storm_queries, generate_storm_query, validate_storm_query, get_form_info.",
				s.store.Len(), s.corpus.Len(), s.forms.Len(), genAvail,
			),
		},
	}
}

func (s *MCPServer) handleToolsList(req jsonRPCRequest) jsonRPCResponse {
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  toolsListResult{Tools: allTools(s.queryGen != nil, s.forms.Len())},
	}
}

func allTools(queryGenAvailable bool, formCount int) []toolDef {
	return []toolDef{
		{
			Name:        "search_synapse_docs",
			Description: "Semantic search over the Synapse documentation. Returns the most relevant passages for a query about Storm syntax, built-ins, or Synapse concepts.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string", "description": "Natural-language search query"},
					"top_k": map[string]interface{}{"type": "number", "description": "Number of results (default 5, max 20)", "default": 5, "minimum": 1, "maximum": 20},
				},
				"required": []string{"query"},
			},
		},
		{
			Name:        "check_storm_code",
			Description: "Analyze Storm code for lint violations using the ML classifier. Returns a label (clean/violation), confidence, predicted rule code, and a feature summary.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"code": map[string]interface{}{"type": "string", "description": "Storm code to analyze"},
				},
				"required": []string{"code"},
			},
		},
		{
			Name:        "get_rule_info",
			Description: "Get detailed information about a specific Storm lint rule including description, causes, examples, and fix guidance.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"code": map[string]interface{}{"type": "string", "description": "Rule code, e.g. W002, I007, S001. Pass 'list' to see all rules."},
				},
				"required": []string{"code"},
			},
		},
		{
			Name:        "get_storm_examples",
			Description: "Search the clean Storm code corpus for examples relevant to a query. Good for 'show me how to use X in Storm' questions.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string", "description": "Search query for code examples"},
					"top_k": map[string]interface{}{"type": "number", "description": "Number of examples to return (default 3, max 10)", "default": 3, "minimum": 1, "maximum": 10},
				},
				"required": []string{"query"},
			},
		},
		{
			Name: "search_storm_queries",
			Description: "Search tagged Storm query examples by what they achieve (result_tag) and/or the Storm patterns they use (technique_tag). " +
				"Pass list_tags=true to discover all available tags. " +
				"Use this when you need a working example of a specific operation or pattern in Storm.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Natural-language description of what you want the query to do (optional if using tag filters)",
					},
					"result_tag": map[string]interface{}{
						"type":        "string",
						"description": "Filter to queries with this result/intent tag, e.g. 'ip_enrichment', 'slack_notification', 'threat_hunting'",
					},
					"technique_tag": map[string]interface{}{
						"type":        "string",
						"description": "Filter to queries using this Storm pattern, e.g. 'for_loop', 'init_block', 'pivoting', 'http_call'",
					},
					"top_k": map[string]interface{}{
						"type":        "number",
						"description": "Number of results to return (default 5, max 15)",
						"default":     5,
						"minimum":     1,
						"maximum":     15,
					},
					"list_tags": map[string]interface{}{
						"type":        "boolean",
						"description": "If true, return all available result_tags and technique_tags instead of searching",
						"default":     false,
					},
				},
			},
		},
		{
			Name: "generate_storm_query",
			Description: func() string {
				if queryGenAvailable {
					return "Generate a Storm query from a natural-language question using the embedded CodeT5 model. " +
						"Returns the generated query, a lex-validation result, and RAG context hits."
				}
				return "Generate a Storm query from a natural-language question (CodeT5 model not embedded; returns error)."
			}(),
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"question": map[string]interface{}{
						"type":        "string",
						"description": "Natural-language question describing what data to find or pivot from in Synapse",
					},
					"max_tokens": map[string]interface{}{
						"type":        "number",
						"description": "Maximum number of output tokens (default 128, max 256)",
						"default":     128,
						"minimum":     16,
						"maximum":     256,
					},
				},
				"required": []string{"question"},
			},
		},
		{
			Name:        "validate_storm_query",
			Description: "Lex-validate a Storm query string. Returns OK/FAIL, token list, and error details for unrecognised characters or unbalanced brackets.",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Storm query string to validate",
					},
				},
				"required": []string{"query"},
			},
		},
		{
			Name: "get_form_info",
			Description: fmt.Sprintf(
				"Look up Synapse data model form definitions (%d forms indexed). "+
					"Pass an exact form name like 'inet:fqdn' for full details, or a partial name/keyword to search.",
				formCount,
			),
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Exact form name (e.g. 'inet:fqdn') or search keyword (e.g. 'fqdn', 'malware', 'threat')",
					},
					"top_k": map[string]interface{}{
						"type":        "number",
						"description": "Max search results when doing a keyword lookup (default 5, max 20)",
						"default":     5,
						"minimum":     1,
						"maximum":     20,
					},
				},
				"required": []string{"name"},
			},
		},
	}
}

func (s *MCPServer) handleToolsCall(req jsonRPCRequest) jsonRPCResponse {
	var params toolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errResponse(req.ID, errInvalidParams, "bad tools/call params")
	}

	switch params.Name {
	case "search_synapse_docs":
		return s.toolSearchDocs(req, params.Arguments)
	case "check_storm_code":
		return s.toolCheckCode(req, params.Arguments)
	case "get_rule_info":
		return s.toolGetRuleInfo(req, params.Arguments)
	case "get_storm_examples":
		return s.toolGetExamples(req, params.Arguments)
	case "search_storm_queries":
		return s.toolSearchQueries(req, params.Arguments)
	case "generate_storm_query":
		return s.toolGenerateQuery(req, params.Arguments)
	case "validate_storm_query":
		return s.toolValidateQuery(req, params.Arguments)
	case "get_form_info":
		return s.toolGetFormInfo(req, params.Arguments)
	default:
		return errResponse(req.ID, errMethodNotFound, "unknown tool: "+params.Name)
	}
}

// ── Tool implementations ───────────────────────────────────────────────────────

func (s *MCPServer) toolSearchDocs(req jsonRPCRequest, rawArgs json.RawMessage) jsonRPCResponse {
	var args searchDocsArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errResponse(req.ID, errInvalidParams, "bad arguments: "+err.Error())
	}
	if strings.TrimSpace(args.Query) == "" {
		return errResponse(req.ID, errInvalidParams, "query must not be empty")
	}
	topK := clampInt(int(args.TopK), 1, 20, 5)

	s.log.Info("search_synapse_docs", "query", args.Query, "top_k", topK)

	queryVec, err := s.emb.Embed(args.Query)
	if err != nil {
		return toolErrorResponse(req.ID, "embedding failed: "+err.Error())
	}
	results, err := s.store.Search(queryVec, topK)
	if err != nil {
		return toolErrorResponse(req.ID, "search failed: "+err.Error())
	}

	out, _ := json.MarshalIndent(results, "", "  ")
	return textResponse(req.ID, string(out))
}

func (s *MCPServer) toolCheckCode(req jsonRPCRequest, rawArgs json.RawMessage) jsonRPCResponse {
	var args checkCodeArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errResponse(req.ID, errInvalidParams, "bad arguments: "+err.Error())
	}
	if strings.TrimSpace(args.Code) == "" {
		return errResponse(req.ID, errInvalidParams, "code must not be empty")
	}

	s.log.Info("check_storm_code", "code_len", len(args.Code))

	feats := ExtractFeatures(args.Code)

	type checkResult struct {
		Label        string             `json:"label"`
		Confidence   float32            `json:"confidence"`
		RuleCode     string             `json:"rule_code,omitempty"`
		PreFiltered  bool               `json:"pre_filtered"`
		Features     map[string]float32 `json:"features"`
	}

	nonzeroFeats := nonzeroFeatures(feats)

	// Quick rule-based pre-filter: if all violation-count features are zero,
	// skip the model and return clean with high confidence.
	if IsDefinitelyClean(feats) {
		result := checkResult{
			Label:       "clean",
			Confidence:  0.95,
			PreFiltered: true,
			Features:    nonzeroFeats,
		}
		out, _ := json.MarshalIndent(result, "", "  ")
		return textResponse(req.ID, string(out))
	}

	isViol, pViol, err := s.clf.Predict(feats)
	if err != nil {
		return toolErrorResponse(req.ID, "classifier error: "+err.Error())
	}

	result := checkResult{
		Features:    nonzeroFeats,
		PreFiltered: false,
	}

	if isViol {
		result.Label = "violation"
		result.Confidence = pViol
		result.RuleCode = inferRuleCode(feats)
	} else {
		result.Label = "clean"
		result.Confidence = 1.0 - pViol
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	return textResponse(req.ID, string(out))
}

func (s *MCPServer) toolGetRuleInfo(req jsonRPCRequest, rawArgs json.RawMessage) jsonRPCResponse {
	var args getRuleArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errResponse(req.ID, errInvalidParams, "bad arguments: "+err.Error())
	}

	code := strings.TrimSpace(args.Code)
	if strings.ToLower(code) == "list" {
		rules := ListRules()
		out, _ := json.MarshalIndent(rules, "", "  ")
		return textResponse(req.ID, string(out))
	}

	rule := GetRule(code)
	if rule == nil {
		return textResponse(req.ID, fmt.Sprintf(`{"error": "unknown rule code %q. Pass 'list' to see all codes."}`, code))
	}

	out, _ := json.MarshalIndent(rule, "", "  ")
	return textResponse(req.ID, string(out))
}

func (s *MCPServer) toolGetExamples(req jsonRPCRequest, rawArgs json.RawMessage) jsonRPCResponse {
	var args getExamplesArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errResponse(req.ID, errInvalidParams, "bad arguments: "+err.Error())
	}
	if strings.TrimSpace(args.Query) == "" {
		return errResponse(req.ID, errInvalidParams, "query must not be empty")
	}
	topK := clampInt(int(args.TopK), 1, 10, 3)

	s.log.Info("get_storm_examples", "query", args.Query, "top_k", topK)

	results := s.corpus.Search(args.Query, topK)
	if len(results) == 0 {
		return textResponse(req.ID, `[]`)
	}

	out, _ := json.MarshalIndent(results, "", "  ")
	return textResponse(req.ID, string(out))
}

func (s *MCPServer) toolSearchQueries(req jsonRPCRequest, rawArgs json.RawMessage) jsonRPCResponse {
	var args searchQueriesArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errResponse(req.ID, errInvalidParams, "bad arguments: "+err.Error())
	}

	// list_tags mode: return the full tag taxonomy
	if args.ListTags {
		type tagList struct {
			ResultTags    []string `json:"result_tags"`
			TechniqueTags []string `json:"technique_tags"`
		}
		out, _ := json.MarshalIndent(tagList{
			ResultTags:    s.tagged.AllResultTags(),
			TechniqueTags: s.tagged.AllTechniqueTags(),
		}, "", "  ")
		return textResponse(req.ID, string(out))
	}

	if strings.TrimSpace(args.Query) == "" && args.ResultTag == "" && args.TechniqueTag == "" {
		return errResponse(req.ID, errInvalidParams,
			"provide at least one of: query, result_tag, technique_tag (or list_tags=true)")
	}

	topK := clampInt(int(args.TopK), 1, 15, 5)

	s.log.Info("search_storm_queries",
		"query", args.Query,
		"result_tag", args.ResultTag,
		"technique_tag", args.TechniqueTag,
		"top_k", topK,
	)

	results := s.tagged.Search(args.Query, args.ResultTag, args.TechniqueTag, topK)
	if len(results) == 0 {
		msg := `{"results": [], "hint": "No matches found. Try list_tags=true to see available tags."}`
		return textResponse(req.ID, msg)
	}

	out, _ := json.MarshalIndent(results, "", "  ")
	return textResponse(req.ID, string(out))
}

func (s *MCPServer) toolGenerateQuery(req jsonRPCRequest, rawArgs json.RawMessage) jsonRPCResponse {
	var args generateQueryArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errResponse(req.ID, errInvalidParams, "bad arguments: "+err.Error())
	}
	if strings.TrimSpace(args.Question) == "" {
		return errResponse(req.ID, errInvalidParams, "question must not be empty")
	}
	if s.queryGen == nil {
		return toolErrorResponse(req.ID, "CodeT5 query generator not available (server built without -tags embedquery)")
	}

	maxTok := clampInt(int(args.MaxTokens), 16, 256, 128)
	s.log.Info("generate_storm_query", "question", args.Question, "max_tokens", maxTok)

	// RAG: retrieve and summarise relevant corpus examples.
	retrieved := s.corpus.Search(args.Question, 3)
	summary := SummarizeResults(retrieved)

	prompt := args.Question
	if summary != "" {
		prompt = summary + "\nquestion: " + args.Question
	}

	query, err := s.queryGen.Generate(prompt, maxTok)
	if err != nil {
		return toolErrorResponse(req.ID, "generation failed: "+err.Error())
	}

	lexResult := StormValidate(query)

	type genResult struct {
		Query      string   `json:"query"`
		LexOK      bool     `json:"lex_ok"`
		LexErrors  []string `json:"lex_errors,omitempty"`
		Tokens     string   `json:"tokens"`
		RAGContext string   `json:"rag_context,omitempty"`
	}
	result := genResult{
		Query:     query,
		LexOK:     lexResult.OK,
		LexErrors: lexResult.Errors,
		Tokens:    FormatTokens(lexResult.Tokens),
	}
	if summary != "" {
		result.RAGContext = summary
	}
	out, _ := json.MarshalIndent(result, "", "  ")
	return textResponse(req.ID, string(out))
}

func (s *MCPServer) toolValidateQuery(req jsonRPCRequest, rawArgs json.RawMessage) jsonRPCResponse {
	var args validateQueryArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errResponse(req.ID, errInvalidParams, "bad arguments: "+err.Error())
	}
	if args.Query == "" {
		return errResponse(req.ID, errInvalidParams, "query must not be empty")
	}

	s.log.Info("validate_storm_query", "query_len", len(args.Query))

	result := StormValidate(args.Query)

	type valResult struct {
		OK     bool     `json:"ok"`
		Tokens string   `json:"tokens"`
		Errors []string `json:"errors,omitempty"`
	}
	out, _ := json.MarshalIndent(valResult{
		OK:     result.OK,
		Tokens: FormatTokens(result.Tokens),
		Errors: result.Errors,
	}, "", "  ")
	return textResponse(req.ID, string(out))
}

func (s *MCPServer) toolGetFormInfo(req jsonRPCRequest, rawArgs json.RawMessage) jsonRPCResponse {
	var args getFormInfoArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return errResponse(req.ID, errInvalidParams, "bad arguments: "+err.Error())
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return errResponse(req.ID, errInvalidParams, "name must not be empty")
	}

	s.log.Info("get_form_info", "name", name)

	// Try exact match first.
	if f := s.forms.Get(name); f != nil {
		out, _ := json.MarshalIndent(map[string]interface{}{
			"name":  name,
			"doc":   f.Doc,
			"ex":    f.Ex,
			"props": f.Props,
		}, "", "  ")
		return textResponse(req.ID, string(out))
	}

	// Fall back to substring search.
	topK := clampInt(int(args.TopK), 1, 20, 5)
	results := s.forms.Search(name, topK)
	if len(results) == 0 {
		return textResponse(req.ID, fmt.Sprintf(`{"error": "no forms found matching %q"}`, name))
	}
	out, _ := json.MarshalIndent(results, "", "  ")
	return textResponse(req.ID, string(out))
}

// ── Helpers ────────────────────────────────────────────────────────────────────

// featureNames mirrors FEATURE_NAMES in train_model.py.
var featureNames = [nFeatures]string{
	"n_lines", "mean_line_len", "max_line_len", "std_line_len",
	"trailing_ws_count", "multi_space_count", "long_line_count", "bad_indent_count",
	"empty_block_count", "single_letter_var_count", "magic_number_count", "hardcoded_cred_count",
	"for_loop_count", "function_def_count", "func_no_doc_count", "has_init_block",
	"lib_set_count", "lib_set_in_init", "has_spin_in_for", "edit_bracket_count",
	"edit_no_spin_count", "has_break_in_for", "pivot_count", "uniq_after_pivot_count",
	"has_globals_get", "http_call_count", "has_parallel", "pipe_count",
	"has_subquery_prop_assign", "trailing_ws_ratio", "multi_space_ratio",
}

func nonzeroFeatures(feats []float32) map[string]float32 {
	m := make(map[string]float32)
	for i, v := range feats {
		if v != 0 {
			m[featureNames[i]] = v
		}
	}
	return m
}

// inferRuleCode picks the most likely rule code based on which violation
// count features are highest. This is a heuristic fallback when the
// multiclass model isn't available.
func inferRuleCode(feats []float32) string {
	type candidate struct {
		idx  int
		rule string
	}
	// Ordered by feature index → rule code mapping
	candidates := []candidate{
		{featTrailingWsCount, "W002"},
		{featMultiSpaceCount, "W003"},
		{featLongLineCount, "W001"},
		{featBadIndentCount, "W007"},
		{featEmptyBlockCount, "W008"},
		{featSingleLetterVarCount, "I001"},
		{featMagicNumberCount, "I004"},
		{featHardcodedCredCount, "S001"},
		{featFuncNoDocCount, "I005"},
		{featEditNoSpinCount, "I007"},
	}
	best := ""
	bestVal := float32(0)
	for _, c := range candidates {
		if feats[c.idx] > bestVal {
			bestVal = feats[c.idx]
			best = c.rule
		}
	}
	if best == "" {
		return "unknown"
	}
	return best
}

func clampInt(v, lo, hi, defaultVal int) int {
	if v <= 0 {
		return defaultVal
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ── Wire framing ───────────────────────────────────────────────────────────────

func (s *MCPServer) readMessage() ([]byte, error) {
	for {
		line, err := s.reader.ReadBytes('\n')
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) > 0 {
			return trimmed, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

func (s *MCPServer) sendResponse(resp jsonRPCResponse) error {
	body, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	s.log.Debug("send", "id", string(resp.ID), "len", len(body))
	_, err = fmt.Fprintf(s.writer, "%s\n", body)
	return err
}

func (s *MCPServer) sendError(id json.RawMessage, code int, msg string) {
	_ = s.sendResponse(errResponse(id, code, msg))
}

func errResponse(id json.RawMessage, code int, msg string) jsonRPCResponse {
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonRPCError{Code: code, Message: msg},
	}
}

func textResponse(id json.RawMessage, text string) jsonRPCResponse {
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: toolCallResult{
			Content: []contentItem{{Type: "text", Text: text}},
		},
	}
}

func toolErrorResponse(id json.RawMessage, msg string) jsonRPCResponse {
	return jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result: toolCallResult{
			Content: []contentItem{{Type: "text", Text: msg}},
			IsError: true,
		},
	}
}
