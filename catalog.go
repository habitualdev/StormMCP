package main

import "strings"

// RuleInfo describes a single Storm lint rule.
type RuleInfo struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
	Causes      string `json:"causes"`
	ExampleBad  string `json:"example_bad"`
	ExampleGood string `json:"example_good"`
	Fix         string `json:"fix"`
}

var rulesCatalog = map[string]RuleInfo{
	"E001": {
		Code: "E001", Name: "Syntax Error", Severity: "error",
		Description: "The Storm code failed to parse. This is a hard error — the query cannot run.",
		Causes:      "Unclosed brackets, misspelled keywords, invalid syntax for Storm operators.",
		ExampleBad:  "inet:ipv4:addr = 1.2.3.4\n| $lib.print($node.value()",
		ExampleGood: "inet:ipv4:addr = 1.2.3.4\n| $lib.print($node.value())",
		Fix:         "Check for unclosed parentheses, brackets, or braces. Verify Storm syntax.",
	},
	"E002": {
		Code: "E002", Name: "Invalid Variable Name", Severity: "error",
		Description: "A variable name starts with `$` but is followed by an invalid character.",
		Causes:      "Using `$1`, `$-var`, or other non-alphanumeric starts.",
		ExampleBad:  "$1count = 0",
		ExampleGood: "$count = 0",
		Fix:         "Variable names must start with a letter or underscore after `$`.",
	},
	"E003": {
		Code: "E003", Name: "Undefined Variable", Severity: "warning",
		Description: "A variable is used before it has been assigned or passed as a parameter.",
		Causes:      "Typo in variable name, using a variable outside its scope, missing `$lib.globals.get()`.",
		ExampleBad:  "$lib.print($result)  // $result never set",
		ExampleGood: "$result = $lib.str.concat('hello', ' world')\n$lib.print($result)",
		Fix:         "Assign the variable before use, or retrieve it from globals with `$lib.globals.get('key')`.",
	},
	"E004": {
		Code: "E004", Name: "Unused Variable", Severity: "info",
		Description: "A variable is assigned but never read. This is dead code.",
		Causes:      "Leftover variable from refactoring, typo in variable name when using it.",
		ExampleBad:  "$temp = $node.value()\n$lib.print('done')",
		ExampleGood: "$temp = $node.value()\n$lib.print($temp)",
		Fix:         "Remove the assignment or use the variable.",
	},
	"E006": {
		Code: "E006", Name: "Invalid Tag Syntax", Severity: "error",
		Description: "A tag expression contains invalid characters.",
		Causes:      "Using special characters in tag names that Storm doesn't allow.",
		ExampleBad:  "#cno.mal.ware!",
		ExampleGood: "#cno.malware",
		Fix:         "Tags may only contain alphanumerics, dots, underscores, and `$` for variable interpolation.",
	},
	"E008": {
		Code: "E008", Name: "Unclosed Bracket", Severity: "error",
		Description: "A `(`, `[`, or `{` is opened but never closed (or vice-versa).",
		Causes:      "Missing closing bracket, mismatched bracket types.",
		ExampleBad:  "if ($x > 0 {\n    $lib.print($x)\n}",
		ExampleGood: "if ($x > 0) {\n    $lib.print($x)\n}",
		Fix:         "Match every opening bracket with its correct closing bracket.",
	},
	"W001": {
		Code: "W001", Name: "Line Too Long", Severity: "warning",
		Description: "A line exceeds 120 characters, harming readability.",
		Causes:      "Long inline expressions, multiple operations on one line.",
		ExampleBad:  "inet:ipv4:addr | $lib.print($node.value()) | $lib.print($node.props.asn) | $lib.print($node.tags())",
		ExampleGood: "inet:ipv4:addr\n| $lib.print($node.value())\n| $lib.print($node.props.asn)\n| $lib.print($node.tags())",
		Fix:         "Break the line using Storm's pipe `|` continuation style, one operation per line.",
	},
	"W002": {
		Code: "W002", Name: "Trailing Whitespace", Severity: "warning",
		Description: "A line has invisible trailing spaces or tabs after the last non-whitespace character.",
		Causes:      "Editor auto-indent, copy-paste from other sources.",
		ExampleBad:  "inet:ipv4:addr   ",
		ExampleGood: "inet:ipv4:addr",
		Fix:         "Remove trailing whitespace. Most editors can do this automatically on save.",
	},
	"W003": {
		Code: "W003", Name: "Multiple Consecutive Spaces", Severity: "warning",
		Description: "Two or more spaces appear in a row where only one is needed.",
		Causes:      "Accidental double-space, misaligned manual indentation.",
		ExampleBad:  "inet:ipv4:addr  = 1.2.3.4",
		ExampleGood: "inet:ipv4:addr = 1.2.3.4",
		Fix:         "Collapse multiple spaces to a single space outside of string literals.",
	},
	"W006": {
		Code: "W006", Name: "Variable Naming Convention", Severity: "info",
		Description: "Variable name doesn't follow camelCase or snake_case convention.",
		Causes:      "Using PascalCase, ALL_CAPS, or mixed conventions.",
		ExampleBad:  "$MyVariable = 1",
		ExampleGood: "$myVariable = 1  // camelCase\n$my_variable = 1  // snake_case",
		Fix:         "Rename to camelCase (`$myVar`) or snake_case (`$my_var`).",
	},
	"W007": {
		Code: "W007", Name: "Inconsistent Indentation", Severity: "warning",
		Description: "Indentation is not a multiple of 4 spaces.",
		Causes:      "Mixing tabs and spaces, using 2-space indent.",
		ExampleBad:  "function doThing() {\n   $x = 1\n}",
		ExampleGood: "function doThing() {\n    $x = 1\n}",
		Fix:         "Use 4-space indentation consistently. Configure your editor to use spaces, not tabs.",
	},
	"W008": {
		Code: "W008", Name: "Empty Code Block", Severity: "warning",
		Description: "A `{ }` block contains no statements.",
		Causes:      "Placeholder code not filled in, accidental empty block.",
		ExampleBad:  "if ($x > 0) {}",
		ExampleGood: "if ($x > 0) {\n    $lib.print($x)\n}",
		Fix:         "Add statements inside the block or remove it entirely.",
	},
	"I001": {
		Code: "I001", Name: "Non-Descriptive Variable Name", Severity: "info",
		Description: "Variable is named with a single letter like `$i`, `$x`, `$k` — hard to understand.",
		Causes:      "Ported from other languages where single-letter loop vars are idiomatic.",
		ExampleBad:  "for $i in $list { $lib.print($i) }",
		ExampleGood: "for $item in $list { $lib.print($item) }",
		Fix:         "Use a name that describes what the variable holds.",
	},
	"I002": {
		Code: "I002", Name: "Long Pipeline", Severity: "info",
		Description: "A single pipeline has more than 5 stages on one line.",
		Causes:      "Chaining too many operations inline.",
		ExampleBad:  "inet:ipv4:addr | +#cno | -> inet:ipv4:subnet | -> inet:asn | $lib.print($node)",
		ExampleGood: "inet:ipv4:addr\n| +#cno\n| -> inet:ipv4:subnet\n| -> inet:asn\n| $lib.print($node)",
		Fix:         "Split the pipeline across lines using the `|` at the start of each new line.",
	},
	"I004": {
		Code: "I004", Name: "Magic Number", Severity: "info",
		Description: "A numeric literal appears directly in code with no explanation.",
		Causes:      "Hardcoded limits, timeouts, counts without named constants.",
		ExampleBad:  "$lib.time.sleep(30)",
		ExampleGood: "$sleepSecs = 30  // wait for rate limit reset\n$lib.time.sleep($sleepSecs)",
		Fix:         "Assign the number to a named variable with a comment explaining its purpose.",
	},
	"I005": {
		Code: "I005", Name: "Missing Function Docstring", Severity: "info",
		Description: "A `function` definition has no `//` comment on the line immediately after the opening brace.",
		Causes:      "Function written without documentation.",
		ExampleBad:  "function enrichIp($ip) {\n    return $lib.inet.dns.lookups($ip)\n}",
		ExampleGood: "function enrichIp($ip) {\n    // Perform DNS lookups for a given IP address.\n    return $lib.inet.dns.lookups($ip)\n}",
		Fix:         "Add a `//` comment as the first line inside the function body.",
	},
	"I007": {
		Code: "I007", Name: "lib.set() Outside Init Block", Severity: "info",
		Description: "`$lib.set()` creates a set object and is expensive. It should only be called inside an `init { }` block so it runs once, not once-per-node.",
		Causes:      "Creating sets inside the main pipeline body.",
		ExampleBad:  "inet:ipv4:addr\n| $seen = $lib.set()\n| $seen.add($node.value())",
		ExampleGood: "init {\n    $seen = $lib.set()\n}\ninet:ipv4:addr\n| $seen.add($node.value())",
		Fix:         "Move `$lib.set()` calls into an `init { }` block at the top of the query.",
	},
	"I008": {
		Code: "I008", Name: "Missing uniq After Pivot", Severity: "info",
		Description: "A pivot (`->`) without a subsequent `| uniq` may produce duplicate nodes in the result set.",
		Causes:      "Multiple source nodes pivoting to the same destination.",
		ExampleBad:  "inet:ipv4:addr\n| -> inet:ipv4:subnet",
		ExampleGood: "inet:ipv4:addr\n| -> inet:ipv4:subnet\n| uniq",
		Fix:         "Add `| uniq` after pivots to deduplicate results.",
	},
	"S001": {
		Code: "S001", Name: "Hardcoded Credential", Severity: "warning",
		Description: "A string literal that looks like a password, API key, secret, or token is hardcoded in the query.",
		Causes:      "Embedding credentials directly instead of using `$lib.globals` or config.",
		ExampleBad:  "$apiKey = 'sk-abc123secret'",
		ExampleGood: "$apiKey = $lib.globals.get('my_service:apikey', default=$lib.null)",
		Fix:         "Store credentials in Synapse globals (`$lib.globals.set`) and retrieve them at runtime.",
	},
	"S002": {
		Code: "S002", Name: "Dangerous Command", Severity: "warning",
		Description: "A command that can permanently modify or delete data (`delnode`, `movenodes`) is present.",
		Causes:      "Destructive operations in a query that might be run unintentionally.",
		ExampleBad:  "inet:ipv4:addr = 1.2.3.4 | delnode",
		ExampleGood: "// Review carefully before using delnode\ninet:ipv4:addr = 1.2.3.4 | delnode",
		Fix:         "Ensure this is intentional. Consider wrapping in a confirmation gate or running with `--dryrun` first.",
	},
}

// GetRule looks up a rule by code (case-insensitive). Returns nil if not found.
func GetRule(code string) *RuleInfo {
	r, ok := rulesCatalog[strings.ToUpper(code)]
	if !ok {
		return nil
	}
	return &r
}

// ListRules returns all rules as a slice, sorted by code.
func ListRules() []RuleInfo {
	codes := []string{
		"E001", "E002", "E003", "E004", "E006", "E008",
		"W001", "W002", "W003", "W006", "W007", "W008",
		"I001", "I002", "I004", "I005", "I007", "I008",
		"S001", "S002",
	}
	out := make([]RuleInfo, 0, len(codes))
	for _, c := range codes {
		if r, ok := rulesCatalog[c]; ok {
			out = append(out, r)
		}
	}
	return out
}
