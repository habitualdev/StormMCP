package main

import (
	"fmt"
	"regexp"
	"strings"
)

// ─── Token types ─────────────────────────────────────────────────────────────

type TokKind uint8

const (
	TokFormName      TokKind = iota // inet:fqdn, file:bytes:sha256  (PROPS)
	TokWildProp                     // inet:fqdn:*
	TokUnivProp                     // .seen, .asn               (UNIVNAME)
	TokRelProp                      // :asn, :fqdn               (RELNAME)
	TokTag                          // #threat.intel.c2
	TokPivotRight                   // ->
	TokPivotLeft                    // <-
	TokJoinRight                    // -+>
	TokJoinLeft                     // <+-
	TokWalkRight                    // -->
	TokWalkLeft                     // <--
	TokWalkJoinRight                // --+>
	TokWalkJoinLeft                 // <+--
	TokEdgeOpen                     // +(   or  -(   or  <(
	TokEdgeClose                    // )>   or  )-   or  )+
	TokPipe                         // |
	TokPlus                         // +
	TokMinus                        // -
	TokEquals                       // =
	TokTrySet                       // ?=  ?+=
	TokModSet                       // +=  -=  ++=  --=
	TokCmpOp                        // <= >= != ~= ^= > <
	TokString                       // "..." '...' '''...'''
	TokNumber                       // 123  0xff  3.14
	TokBool                         // true false
	TokNull                         // null
	TokVar                          // $varname
	TokStar                         // *
	TokLBrace                       // {
	TokRBrace                       // }
	TokLBracket                     // [
	TokRBracket                     // ]
	TokLParen                       // (
	TokRParen                       // )
	TokComma                        // ,
	TokWord                         // cmd names, bare values, identifiers
	TokWS                           // whitespace (skipped in output)
	TokUnknown                      // anything unrecognised
)

var tokKindName = [...]string{
	TokFormName:      "FormName",
	TokWildProp:      "WildProp",
	TokUnivProp:      "UnivProp",
	TokRelProp:       "RelProp",
	TokTag:           "Tag",
	TokPivotRight:    "->",
	TokPivotLeft:     "<-",
	TokJoinRight:     "-+>",
	TokJoinLeft:      "<+-",
	TokWalkRight:     "-->",
	TokWalkLeft:      "<--",
	TokWalkJoinRight: "--+>",
	TokWalkJoinLeft:  "<+--",
	TokEdgeOpen:      "EdgeOpen",
	TokEdgeClose:     "EdgeClose",
	TokPipe:          "|",
	TokPlus:          "+",
	TokMinus:         "-",
	TokEquals:        "=",
	TokTrySet:        "TrySet",
	TokModSet:        "ModSet",
	TokCmpOp:         "CmpOp",
	TokString:        "String",
	TokNumber:        "Number",
	TokBool:          "Bool",
	TokNull:          "Null",
	TokVar:           "Var",
	TokStar:          "*",
	TokLBrace:        "{",
	TokRBrace:        "}",
	TokLBracket:      "[",
	TokRBracket:      "]",
	TokLParen:        "(",
	TokRParen:        ")",
	TokComma:         ",",
	TokWord:          "Word",
	TokWS:            "WS",
	TokUnknown:       "?",
}

func (k TokKind) String() string {
	if int(k) < len(tokKindName) {
		return tokKindName[k]
	}
	return fmt.Sprintf("Tok(%d)", int(k))
}

type Token struct {
	Kind  TokKind
	Value string
	Pos   int
}

func (t Token) String() string {
	switch t.Kind {
	case TokWS:
		return ""
	case TokPivotRight, TokPivotLeft, TokJoinRight, TokJoinLeft,
		TokWalkRight, TokWalkLeft, TokWalkJoinRight, TokWalkJoinLeft,
		TokPipe, TokPlus, TokMinus, TokEquals, TokStar,
		TokLBrace, TokRBrace, TokLBracket, TokRBracket, TokLParen, TokRParen, TokComma:
		return fmt.Sprintf("[%s]", t.Kind)
	default:
		return fmt.Sprintf("[%s:%s]", t.Kind, t.Value)
	}
}

// ─── Rules (tried in order at each position) ─────────────────────────────────

type lexRule struct {
	re   *regexp.Regexp
	kind TokKind
}

var lexRules []lexRule

func init() {
	defs := []struct {
		pat  string
		kind TokKind
	}{
		// Multi-char operators — longest match first.
		{`--\+>`, TokWalkJoinRight},
		{`<\+--`, TokWalkJoinLeft},
		{`-->`, TokWalkRight},
		{`<--`, TokWalkLeft},
		{`-\+>`, TokJoinRight},
		{`<\+-`, TokJoinLeft},
		{`->`, TokPivotRight},
		{`<-`, TokPivotLeft},
		// Edge-walk notation
		{`\+\(`, TokEdgeOpen},
		{`-\(`, TokEdgeOpen},
		{`<\(`, TokEdgeOpen},
		{`\)>`, TokEdgeClose},
		{`\)-`, TokEdgeClose},
		{`\)\+`, TokEdgeClose},
		// Strings — triple-quoted first.
		{`'''[\s\S]*?'''`, TokString},
		{"\"(?:[^\"\\\\]|\\\\.)*\"", TokString},
		{`'[^']*'`, TokString},
		// Try/mod-set operators before bare +/-.
		{`\?\+?=`, TokTrySet},
		{`[+\-]{1,2}[+\-]?=`, TokModSet},
		// Comparison operators.
		{`(?:<=|>=|!=|~=|\^=)`, TokCmpOp},
		// Tag — before # is used as anything else.
		{`#[\w]+(\.[\w*]+)*`, TokTag},
		// Wildcard property (WILDPROPS) — before plain PROPS.
		{`[a-z_][a-z0-9_]*(?::[a-z0-9_]+)+(?:[:.][a-z0-9_]+)*:\*`, TokWildProp},
		// Form name (PROPS) — must have at least one colon.
		{`[a-z_][a-z0-9_]*(?::[a-z0-9_]+)+(?:[:.][a-z0-9_]+)*`, TokFormName},
		// Universal property (starts with dot, context-sensitive — best effort).
		{`\.[a-z_][a-z0-9_]*(?:[:.][a-z0-9_]+)*`, TokUnivProp},
		// Relative property (starts with colon).
		{`:\.?[a-z_][a-z0-9_]*(?:(?:::|\:|\.)[a-z_][a-z0-9_]*)*`, TokRelProp},
		// Variable.
		{`\$\w+`, TokVar},
		// Numbers (no sign — a leading minus is TokMinus then TokNumber).
		{`0x[0-9a-fA-F]+`, TokNumber},
		{`\d+(?:\.\d*)?`, TokNumber},
		// Bool / null keywords.
		{`(?:true|false)(?:[^\w]|$)`, TokBool},
		{`null(?:[^\w]|$)`, TokNull},
		// Whitespace.
		{`\s+`, TokWS},
		// Single-char punctuation.
		{`\|`, TokPipe},
		{`=`, TokEquals},
		{`>`, TokCmpOp},
		{`<`, TokCmpOp},
		{`\+`, TokPlus},
		{`-`, TokMinus},
		{`\*`, TokStar},
		{`\{`, TokLBrace},
		{`\}`, TokRBrace},
		{`\[`, TokLBracket},
		{`\]`, TokRBracket},
		{`\(`, TokLParen},
		{`\)`, TokRParen},
		{`,`, TokComma},
		// Catch-all word/value (cmd names, bare strings, IPs, hashes, …).
		{`[a-zA-Z_][a-zA-Z0-9_.]*`, TokWord},
		{`[^\s\|\{\}\[\]\(\),]+`, TokWord},
	}
	lexRules = make([]lexRule, len(defs))
	for i, d := range defs {
		lexRules[i] = lexRule{regexp.MustCompile(`^(?:` + d.pat + `)`), d.kind}
	}
}

// ─── Tokenize ────────────────────────────────────────────────────────────────

// StormTokenize breaks a Storm query string into tokens.
func StormTokenize(input string) []Token {
	var tokens []Token
	pos := 0
	s := input
	for len(s) > 0 {
		matched := false
		for _, r := range lexRules {
			loc := r.re.FindStringIndex(s)
			if loc == nil || loc[0] != 0 {
				continue
			}
			val := s[loc[0]:loc[1]]

			// Bool/null patterns over-consume by one char to enforce word boundary;
			// push that char back.
			kind := r.kind
			if kind == TokBool || kind == TokNull {
				if len(val) > 0 && !isLexWordChar(rune(val[len(val)-1])) {
					val = val[:len(val)-1]
				}
			}

			tokens = append(tokens, Token{Kind: kind, Value: val, Pos: pos})
			pos += len(val)
			s = s[len(val):]
			matched = true
			break
		}
		if !matched {
			tokens = append(tokens, Token{Kind: TokUnknown, Value: string(s[0]), Pos: pos})
			pos++
			s = s[1:]
		}
	}
	return tokens
}

func isLexWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// ─── Validate ────────────────────────────────────────────────────────────────

// LexResult holds the outcome of a lex-validation pass.
type LexResult struct {
	OK     bool
	Tokens []Token
	Errors []string
}

// StormValidate tokenizes the query and checks for unknown characters and
// unbalanced braces / brackets / parens.
func StormValidate(query string) LexResult {
	tokens := StormTokenize(query)
	var errs []string

	for _, t := range tokens {
		if t.Kind == TokUnknown {
			errs = append(errs, fmt.Sprintf("unrecognised character %q at pos %d", t.Value, t.Pos))
		}
	}

	type bracket struct {
		ch  byte
		pos int
	}
	var stack []bracket
	pairs := map[byte]byte{'}': '{', ']': '[', ')': '('}
	opens := map[byte]bool{'{': true, '[': true, '(': true}

	for _, t := range tokens {
		if t.Kind == TokWS {
			continue
		}
		if len(t.Value) == 1 {
			ch := t.Value[0]
			if opens[ch] {
				stack = append(stack, bracket{ch, t.Pos})
			} else if want, ok := pairs[ch]; ok {
				if len(stack) == 0 || stack[len(stack)-1].ch != want {
					errs = append(errs, fmt.Sprintf("unmatched %q at pos %d", ch, t.Pos))
				} else {
					stack = stack[:len(stack)-1]
				}
			}
		}
	}
	for _, b := range stack {
		errs = append(errs, fmt.Sprintf("unclosed %q opened at pos %d", b.ch, b.pos))
	}

	return LexResult{OK: len(errs) == 0, Tokens: tokens, Errors: errs}
}

// FormatTokens returns a compact inline token listing, skipping whitespace.
func FormatTokens(tokens []Token) string {
	var sb strings.Builder
	for _, t := range tokens {
		if t.Kind == TokWS {
			continue
		}
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(t.String())
	}
	return sb.String()
}
