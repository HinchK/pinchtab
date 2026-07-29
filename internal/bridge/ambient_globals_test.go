package bridge

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// An isolated-world node handle lives in the TOP frame's isolated context, so
// inside a Runtime.callFunctionOn declaration the ambient window/document are the
// top frame's, not the node's. A frame-offset walk started from the ambient window
// terminates on its first iteration and yields frame-local coordinates; a hit test
// there runs against the wrong document. Both fail silently.
//
// This census is the enforcement half of that rule. Its sibling,
// TestIsolatedWorldBoundaryCensus, covers WHICH element is acted on; this one
// covers what the declaration is allowed to read once it has one.
//
// PRESENCE, NOT ABSENCE. It does not try to prove a bare window/document is
// shadowed — that is an absence heuristic over JS inside Go string literals and
// fails open on any wording it did not anticipate, which is exactly the shape of
// the near miss it exists to catch (`const view = window;` landing under a comment
// asserting the opposite). Instead: if a declaration mentions an ambient global at
// all, it must carry a node-derived shadow line verbatim. A hand-rolled variant
// fails closed.
//
// GRANULARITY: per file, not per call site. Correlating a declaration with whether
// its handle came from IsolatedNodeObjectID is not reliably textual, so a file that
// obtains isolated handles puts every callFunctionOn declaration in it in scope.
//
// FALSE POSITIVES point one way, and that way is safe: a main-world declaration
// sitting in such a file is asked to shadow too. Shadowing is correct in either
// world — it costs one line and changes no behaviour. The census never excuses a
// declaration that should shadow; it can only ask one that need not to.
//
// THE CANONICAL LINES STAY INLINE, four copies of one line, deliberately. Hoisting
// them into a shared const would blind this census, because what it matches is text
// inside the literal it scans. Four copies held identical by a test that fails on
// drift beats one const the guard cannot see. If declaration assembly ever becomes
// first-class, revisit both together.

// isolatedHandleTokens put a file in scope: it either produces an isolated-world
// handle or obtains one. Naming the producers rather than a directory is what
// keeps the census module-wide — pinchtab spreads CDP work over internal/bridge,
// internal/bridge/cdpops and internal/cdptk.
var isolatedHandleTokens = []string{"IsolatedNodeObjectID", "FrameExecutionContextID"}

// shadowForm is one accepted way to derive a global from the node. requires is
// extra text that must also be present for the form to be node-derived at all:
// the root spelling is only sound because the declaration binds root to this.
type shadowForm struct {
	line     string
	requires string
}

// ambientShadows is the rule as data: mention the global, carry one of its forms.
// Entries are compliance, not exemption — every one derives the global from the
// node the handle points at.
var ambientShadows = map[string][]shadowForm{
	"window": {
		{line: "const view = (this.ownerDocument && this.ownerDocument.defaultView) || window;"},
	},
	"document": {
		{line: "const doc = this.ownerDocument || document;"},
		{line: "const document = root.ownerDocument || root;", requires: "const root = this;"},
	},
}

// ambientGlobalsExceptions is empty, and the first entry ever added has to be
// argued on the harm it accepts, not on how reasonable the wording sounds. It is
// checked in both directions like the resolution census: a listed file that no
// longer violates the rule fails here too, so an exception cannot outlive its
// reason.
var ambientGlobalsExceptions = map[string]string{}

// canonicalViewLine is the preamble as it stands in the tree. Counting its
// occurrences module-wide against the occurrences the scan actually reached is
// what closes the parser's blind spot: a declaration assembled in a shape this
// census cannot recognise stops being scanned, and that divergence fails.
const canonicalViewLine = "const view = (this.ownerDocument && this.ownerDocument.defaultView) || window;"

type jsLiteral struct {
	text      string
	startLine int
}

// plusChain matches the Go source between two raw literals that are concatenated,
// including any named fragments in the chain: `head` + jsNormalizeHelper + `tail`
// is one declaration, and the xpath arm of the selector resolver lives in the tail.
// Missing this shape is not a cosmetic gap — the fragment that actually reads a
// global is routinely not the fragment that opens the function.
var plusChain = regexp.MustCompile(`^\s*\+\s*(?:[A-Za-z_][A-Za-z0-9_.]*\s*\+\s*)*$`)

var identLiteral = regexp.MustCompile("(?s)([A-Za-z_][A-Za-z0-9_]*)\\s*(?::?=)\\s*`([^`]*)`")

// goRawLiterals returns every backtick literal in a Go source file, splicing
// +-concatenated ones — named fragments included, resolved to their own literal —
// into the single string the browser is actually handed.
func goRawLiterals(src string) []jsLiteral {
	fragments := map[string]string{}
	for _, m := range identLiteral.FindAllStringSubmatch(src, -1) {
		fragments[m[1]] = m[2]
	}

	var literals []jsLiteral
	prevEnd := 0
	line := 1
	for i := 0; i < len(src); i++ {
		if src[i] == '\n' {
			line++
			continue
		}
		if src[i] != '`' {
			continue
		}
		end := strings.IndexByte(src[i+1:], '`')
		if end < 0 {
			break
		}
		body := src[i+1 : i+1+end]
		startLine := line
		line += strings.Count(body, "\n")

		between := src[prevEnd:i]
		if last := len(literals) - 1; last >= 0 && plusChain.MatchString(between) {
			for _, name := range identsInChain(between) {
				literals[last].text += fragments[name]
			}
			literals[last].text += body
		} else {
			literals = append(literals, jsLiteral{text: body, startLine: startLine})
		}
		prevEnd = i + end + 2
		i = prevEnd - 1
	}
	return literals
}

var identToken = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.]*`)

func identsInChain(chain string) []string {
	return identToken.FindAllString(chain, -1)
}

// declarations returns the literals that are Runtime.callFunctionOn function
// declarations: a bare function expression. An IIFE (`(function(`) is a
// Runtime.evaluate payload — no node handle, so the rule does not apply and the
// census never sees it. Excluded by scope beats excluded by exception.
func callFunctionDeclarations(literals []jsLiteral) []jsLiteral {
	var decls []jsLiteral
	for _, lit := range literals {
		head := strings.TrimLeft(lit.text, " \t\n")
		if strings.HasPrefix(head, "function(") || strings.HasPrefix(head, "function (") {
			decls = append(decls, lit)
		}
	}
	return decls
}

// ambientMention reports the offset of the first mention of name that is not a field
// access or part of a longer identifier, so ownerDocument and el.document never
// count as ambient reads.
func ambientMention(text, name string) int {
	for from := 0; ; {
		idx := strings.Index(text[from:], name)
		if idx < 0 {
			return -1
		}
		at := from + idx
		from = at + len(name)
		// A dot BEFORE the name is a field access (el.ownerDocument); a dot AFTER it
		// is the ambient read itself (document.evaluate), which is the whole point.
		// Treating both the same way is how a detector reports mentions it never
		// actually looks for.
		if at > 0 && (text[at-1] == '.' || isIdentByte(text[at-1])) {
			continue
		}
		if from < len(text) && isIdentByte(text[from]) {
			continue
		}
		return at
	}
}

func isIdentByte(b byte) bool {
	return b == '_' || b == '$' ||
		(b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// jsCode blanks whole-line JS comments, keeping the line structure. It matters in
// both directions: prose mentioning window is not an ambient read, and a
// commented-out preamble must not satisfy the rule — that commenting-out is the
// exact near miss this census exists to catch. Only full-line comments are
// blanked, never a trailing one, because eating code would make the census fail
// open, and a trailing comment can only ever cost a harmless extra shadow.
func jsCode(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		for _, prefix := range []string{"//", "/*", "*/", "*"} {
			if strings.HasPrefix(trimmed, prefix) {
				lines[i] = strings.Repeat(" ", len(line))
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}

func lineAtOffset(text string, offset int) string {
	start := strings.LastIndexByte(text[:offset], '\n') + 1
	end := strings.IndexByte(text[start:], '\n')
	if end < 0 {
		return strings.TrimSpace(text[start:])
	}
	return strings.TrimSpace(text[start : start+end])
}

// sourceLine locates the offending JS line back in the Go file. A declaration
// spliced from concatenated fragments does not lay out like the source it came
// from, so counting newlines inside the merged text reports a line the reader
// cannot find; the line's own text can be.
func sourceLine(src, offending string, fallback int) int {
	if offending == "" {
		return fallback
	}
	at := strings.Index(src, offending)
	if at < 0 {
		return fallback
	}
	return strings.Count(src[:at], "\n") + 1
}

func hasShadow(text string, forms []shadowForm) bool {
	for _, form := range forms {
		if !strings.Contains(text, form.line) {
			continue
		}
		if form.requires != "" && !strings.Contains(text, form.requires) {
			continue
		}
		return true
	}
	return false
}

func TestIsolatedHandleDeclarationsShadowAmbientGlobals(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	scopedFiles := 0
	checkedDecls := 0
	exercised := map[string]int{}
	violations := map[string]bool{}
	canonicalInScope := 0
	canonicalInModule := 0

	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		name := filepath.ToSlash(rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(raw)
		canonicalInModule += strings.Count(jsCode(src), canonicalViewLine)

		inScope := false
		for _, token := range isolatedHandleTokens {
			if strings.Contains(src, token) {
				inScope = true
				break
			}
		}
		if !inScope {
			return nil
		}
		scopedFiles++

		for _, decl := range callFunctionDeclarations(goRawLiterals(src)) {
			checkedDecls++
			body := jsCode(decl.text)
			canonicalInScope += strings.Count(body, canonicalViewLine)
			for ambient, forms := range ambientShadows {
				at := ambientMention(body, ambient)
				if at < 0 {
					continue
				}
				exercised[ambient]++
				if hasShadow(body, forms) {
					continue
				}
				violations[name] = true
				offending := lineAtOffset(body, at)
				line := sourceLine(src, offending, decl.startLine)
				if why, excused := ambientGlobalsExceptions[name]; excused {
					t.Logf("%s:%d reads ambient %s under a recorded exception (%s)", name, line, ambient, why)
					continue
				}
				t.Errorf("%s:%d reads the ambient %s inside a callFunctionOn declaration without deriving it from the node:\n  %s\n"+
					"An isolated handle runs in the TOP frame's world, so the ambient %s is not the node's — frame offsets vanish and hit tests run on the wrong document.\n"+
					"Add one of:\n  %s",
					name, line, ambient, offending, ambient,
					strings.Join(formLines(forms), "\n  "))
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}

	t.Logf("census scope: %d files obtain isolated handles, %d callFunctionOn declarations scanned, ambient mentions %v, canonical preamble %d/%d in scope",
		scopedFiles, checkedDecls, exercised, canonicalInScope, canonicalInModule)
	if scopedFiles == 0 {
		t.Error("no file in the module obtains an isolated node handle — the census is checking nothing")
	}
	if checkedDecls == 0 {
		t.Error("no callFunctionOn declaration found in the files that obtain isolated handles — the census is checking nothing")
	}
	// Per ambient global, not in total: one of them falling to zero mentions is how
	// a whole half of the rule stops being scanned while the census stays green.
	for ambient := range ambientShadows {
		if exercised[ambient] == 0 {
			t.Errorf("no declaration mentions the ambient %s any more; drop it from ambientShadows so the recorded rule stays true", ambient)
		}
	}
	// The parser recognises a declaration by its shape. If the preamble turns up in
	// the module somewhere the scan never reached, that shape is a blind spot.
	if canonicalInScope != canonicalInModule {
		t.Errorf("the canonical preamble appears %d times in the module but only %d were inside a declaration this census recognises; a declaration is assembled in a shape the scan cannot see",
			canonicalInModule, canonicalInScope)
	}
	for name, why := range ambientGlobalsExceptions {
		if !violations[name] {
			t.Errorf("%s no longer reads an ambient global unshadowed (%s); remove it from ambientGlobalsExceptions so the recorded exceptions stay true", name, why)
		}
	}
}

func formLines(forms []shadowForm) []string {
	lines := make([]string, 0, len(forms))
	for _, form := range forms {
		if form.requires != "" {
			lines = append(lines, form.line+"   (only with "+form.requires+")")
			continue
		}
		lines = append(lines, form.line)
	}
	return lines
}
