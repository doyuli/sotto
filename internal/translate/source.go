package translate

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"sotto/internal/config"
)

var sourceNewlines = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\u0085", "\n", "\u2028", "\n", "\u2029", "\n")
var lineBreaks = regexp.MustCompile(`[ \t]*(?:\n[ \t]*)+`)

// PrepareSource strips opt-in formatting before language detection. Keep
// line breaks until last because both markup and comments can be line-based.
func PrepareSource(text string, options config.IdentifierSettings) string {
	if !options.StripMarkdown && !options.StripCodeComments && !options.FlattenNewlines {
		return text
	}
	text = sourceNewlines.Replace(text)
	if options.StripMarkdown || options.StripCodeComments {
		text = cleanSourceBlocks(text, options)
	}
	if options.FlattenNewlines {
		text = lineBreaks.ReplaceAllString(text, " ")
	}
	return strings.TrimSpace(text)
}

type commentStyle int

const (
	mixedComments commentStyle = iota
	slashComments
	hashComments
	pythonComments
)

const codeURLs = `[[:alpha:]][[:alnum:]+.\-]*://[^\s"'<>]+`
const codeTripleStrings = `"""(?:\\(?s:.)|[^"\\]|"[^"]|""[^"])*"""|'''(?:\\(?s:.)|[^'\\]|'[^']|''[^'])*'''`
const codeStrings = `"(?:\\[^\n]|[^"\\\n])*"|'(?:\\[^\n]|[^'\\\n])*'`
const shellStrings = `"(?:\\(?s:.)|[^"\\])*"|'[^']*'`
const codeBackticks = "``[^\n]+?``|`[^`]+`"

// Regex literals follow an expression opener. Requiring that context avoids
// treating a division expression followed by a slash comment as a regex.
const codeRegexAtom = `(?:\\[^\n]|\[(?:\\[^\n]|[^\]\\\n])*\]|[^/\\[\n])`
const codeRegex = `(?:^|[=(,:;\[!&|?{}]|\b(?:return|throw|yield|case)[ \t]+)[ \t]*/(?:\\[^\n]|\[(?:\\[^\n]|[^\]\\\n])*\]|[^/\\[\n*+?])` + codeRegexAtom + `*/[dgimsuvy]*`

const codeLiterals = codeURLs + "|" + codeTripleStrings + "|" + codeStrings + "|" + codeBackticks + "|" + codeRegex
const shellLiterals = codeURLs + "|" + codeTripleStrings + "|" + shellStrings + "|" + codeBackticks

const slashComment = `//[^\n]*|/\*(?:[^*]|\*+[^*/])*\*+/`
const mixedHashComment = `(?:^|[ \t]+)#(?:[\pL_#!]|[ \t]+)[^\n]*`

// Untagged snippets can contain Python floor division. Keep clearly numeric
// expressions while still recognizing slash comments on their own line.
const numericFloorDivision = `(?:\b[[:alnum:]_]+|[)\]])[ \t]*//[ \t]*[+-]?[0-9]+(?:\.[0-9]+)?`

var commentTokens = [...]*regexp.Regexp{
	mixedComments:  regexp.MustCompile("(?m)" + codeLiterals + "|" + numericFloorDivision + "|" + slashComment + "|" + mixedHashComment),
	slashComments:  regexp.MustCompile("(?m)" + codeLiterals + "|" + slashComment),
	hashComments:   regexp.MustCompile("(?m)" + shellLiterals + `|(?:^|[ \t]+)#[^\n]*`),
	pythonComments: regexp.MustCompile("(?m)" + codeLiterals + `|[ \t]*#[^\n]*`),
}

// A blank line after a hash heading separates it from a following example.
const codePrefix = `^[ \t]*(?:\n[ \t]*)*(?:#[^\n]*\n[ \t]*)*`

var (
	// Whitespace after '#' denotes a comment here, even before words such as
	// "if" or "error". Tagged C-family blocks already preserve directives.
	codeDirective     = regexp.MustCompile(`^#(?:define|elif|else|endif|error|if|ifdef|ifndef|include|line|pragma|undef|warning)\b`)
	pythonSource      = regexp.MustCompile(`(?m)^[ \t]*(?:async[ \t]+def[ \t]+|def[ \t]+\w+[ \t]*\(|class[ \t]+\w+[^\n]*:|from[ \t]+[\w.]+[ \t]+import[ \t]+|import[ \t]+[\w.]+[ \t]*(?:#.*)?$|print[ \t]*\()`)
	codeAssignment    = regexp.MustCompile(codePrefix + `(?:(?:const|let|var|val)[ \t]+)?[[:alpha:]_]\w*[ \t]*:?=[ \t]*[^=\s]`)
	codePreamble      = regexp.MustCompile(codePrefix + `$`)
	commentDecoration = regexp.MustCompile(`(?m)^[ \t]*\*(?:[ \t]|$)`)

	markdownFence    = regexp.MustCompile("^[ \t]{0,3}(" + "`{3,}|~{3,}" + ")(.*)$")
	fenceTokens      = regexp.MustCompile("(?m)" + markdownFence.String() + "|" + codeLiterals)
	shellFenceTokens = regexp.MustCompile("(?m)" + markdownFence.String() + "|" + shellLiterals)
	markdownQuote    = regexp.MustCompile(`^(?:[ \t]{0,3}>[ \t]?)+`)
	markdownHeading  = regexp.MustCompile(`^[ \t]{0,3}#{1,6}[ \t]+(.*?)(?:[ \t]+#+[ \t]*)?$`)
	markdownList     = regexp.MustCompile(`^[ \t]*(?:[-+*]|[0-9]{1,9}[.)])[ \t]+(?:\[[ xX]\][ \t]+)?`)
	markdownRule     = regexp.MustCompile(`^[ \t]{0,3}(?:\*(?:[ \t]*\*){2,}|-(?:[ \t]*-){2,}|_(?:[ \t]*_){2,})[ \t]*$`)
	markdownInline   = regexp.MustCompile("``([^`].*?)``|`([^`]+)`" + `|!?\[([^\[\]]+)\]\((?:<[^<>]+>|(?:[^()\s]|\([^()\s]*\))+)(?:[ \t]+"[^"]*")?\)|\\([[:punct:]])|\x00`)
	markdownStrike   = regexp.MustCompile(`~~([^~\n]+)~~`)
)

func cleanSourceBlock(text string, style commentStyle, fenced bool, options config.IdentifierSettings) string {
	codeContext := fenced || codeAssignment.MatchString(text)
	if style == mixedComments {
		match := pythonSource.FindStringIndex(text)
		if match != nil && (codeContext || codePreamble.MatchString(text[:match[0]])) {
			style, codeContext = pythonComments, true
		}
	}
	if fenced && !options.StripCodeComments {
		return text
	}

	// Protect individual literals and processed comments, while allowing
	// Markdown cleanup everywhere else. Pick a prefix absent from the input.
	prefix := "\x00source:"
	for strings.Contains(text, prefix) {
		prefix += "\x00"
	}
	var replacements []string
	keep := func(value string) string {
		marker := prefix + strconv.Itoa(len(replacements)/2) + prefix
		replacements = append(replacements, marker, value)
		return marker
	}
	var out strings.Builder
	end := 0
	for _, match := range commentTokens[style].FindAllStringIndex(text, -1) {
		start := match[0]
		out.WriteString(text[end:start])
		end = match[1]
		token := text[start:end]
		marker := strings.TrimLeft(token, " \t")
		body := ""
		switch {
		case strings.HasPrefix(token, "//"):
			body = strings.TrimLeft(token, "/")
			if strings.HasPrefix(body, "! ") {
				body = body[1:]
			}
		case strings.HasPrefix(token, "/*"):
			body = strings.TrimSuffix(strings.TrimPrefix(token, "/*"), "*/")
			if strings.HasPrefix(body, "*") && !strings.HasPrefix(body, "**") {
				body = body[1:]
			}
			body = commentDecoration.ReplaceAllString(body, "")
		case strings.HasPrefix(marker, "#"):
			lineStart := strings.LastIndexByte(text[:start], '\n') + 1
			if strings.HasPrefix(marker, "#!") || (style == mixedComments && codeDirective.MatchString(marker)) ||
				(options.StripMarkdown && !fenced && markdownHeading.MatchString(markdownQuote.ReplaceAllString(text[lineStart:end], ""))) {
				out.WriteString(token)
				continue
			}
			body = strings.TrimLeft(marker, "#")
		default:
			quoted := strings.HasPrefix(token, "\"") || strings.HasPrefix(token, "'") || strings.HasPrefix(token, "`")
			url := !quoted && strings.Contains(token, "://")
			strongLiteral := (!quoted && !url) ||
				strings.HasPrefix(token, "\"\"\"") || strings.HasPrefix(token, "'''")
			if options.StripMarkdown && !url && (codeContext || strongLiteral) {
				token = keep(token)
			}
			out.WriteString(token)
			continue
		}
		if !options.StripCodeComments {
			out.WriteString(token)
			continue
		}
		body = strings.TrimSpace(body)
		if options.StripMarkdown {
			body = stripMarkdown(body)
			if body != "" {
				body = keep(body)
			}
		}
		// Delimiters can be the only separator between code and comment text.
		if start > 0 {
			previous, _ := utf8.DecodeLastRuneInString(text[:start])
			if !unicode.IsSpace(previous) {
				out.WriteByte(' ')
			}
		}
		out.WriteString(body)
		if strings.HasPrefix(token, "/*") && end < len(text) {
			next, _ := utf8.DecodeRuneInString(text[end:])
			if !unicode.IsSpace(next) && body != "" {
				out.WriteByte(' ')
			}
		}
	}
	out.WriteString(text[end:])
	text = out.String()
	if options.StripMarkdown && !fenced {
		text = stripMarkdown(text)
	}
	if len(replacements) > 0 {
		text = strings.NewReplacer(replacements...).Replace(text)
	}
	return text
}

// Scan literals and fences together, so a fence written inside a string
// cannot split that string and expose its contents to comment cleanup.
func cleanSourceBlocks(text string, options config.IdentifierSettings) string {
	var out []string
	var fence string
	style := mixedComments
	start, scan := 0, 0
	writeBlock := func(end int) {
		if start < end {
			block := strings.TrimSuffix(text[start:end], "\n")
			out = append(out, cleanSourceBlock(block, style, fence != "", options))
		}
	}
	for scan < len(text) {
		tokens := fenceTokens
		if style == hashComments {
			tokens = shellFenceTokens
		}
		offset := scan
		match := tokens.FindStringSubmatchIndex(text[offset:])
		if match == nil {
			break
		}
		tokenStart, tokenEnd := offset+match[0], offset+match[1]
		scan = tokenEnd
		if match[2] < 0 || (tokenStart > 0 && text[tokenStart-1] != '\n') {
			continue
		}
		delimiter := text[offset+match[2] : offset+match[3]]
		tag := strings.TrimSpace(text[offset+match[4] : offset+match[5]])
		if fence != "" && (!strings.HasPrefix(delimiter, fence) || tag != "") {
			continue
		}
		writeBlock(tokenStart)
		if !options.StripMarkdown {
			out = append(out, text[tokenStart:tokenEnd])
		}
		if fence == "" {
			fence = delimiter
			tag = strings.ToLower(tag)
			if i := strings.IndexAny(tag, " \t"); i >= 0 {
				tag = tag[:i]
			}
			switch tag {
			case "py", "python", "python3":
				style = pythonComments
			case "sh", "bash", "zsh", "shell", "fish", "ruby", "rb", "yaml", "yml", "toml":
				style = hashComments
			case "c", "c++", "cpp", "csharp", "cs", "go", "golang", "java", "js", "javascript", "ts", "typescript", "rust", "rs", "swift", "kotlin", "kt":
				style = slashComments
			}
		} else {
			fence, style = "", mixedComments
		}
		start = tokenEnd
		if start < len(text) && text[start] == '\n' {
			start++
		}
		scan = start
	}
	writeBlock(len(text))
	return strings.Join(out, "\n")
}

func stripMarkdown(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		line = markdownQuote.ReplaceAllString(line, "")
		switch {
		case markdownRule.MatchString(line):
			line = ""
		case markdownHeading.MatchString(line):
			line = markdownHeading.ReplaceAllString(line, "$1")
		default:
			line = markdownList.ReplaceAllString(line, "")
		}
		lines[i] = stripMarkdownInline(line)
	}
	return strings.Join(lines, "\n")
}

func stripMarkdownInline(line string) string {
	// Mask inline content while stripping outer emphasis. Keep existing NULs
	// in the same list so the temporary markers cannot collide with the input.
	var literals []string
	var masked strings.Builder
	start := 0
	for _, m := range markdownInline.FindAllStringSubmatchIndex(line, -1) {
		masked.WriteString(line[start:m[0]])
		value := line[m[0]:m[1]]
		switch {
		case m[2] >= 0:
			value = line[m[2]:m[3]]
		case m[4] >= 0:
			value = line[m[4]:m[5]]
		case m[6] >= 0:
			value = stripMarkdownInline(line[m[6]:m[7]])
		case m[8] >= 0:
			value = line[m[8]:m[9]]
		}
		literals = append(literals, value)
		masked.WriteByte(0)
		start = m[1]
	}
	masked.WriteString(line[start:])
	line = stripMarkdownEmphasis(masked.String())
	line = markdownStrike.ReplaceAllString(line, "$1")
	if len(literals) == 0 {
		return line
	}
	var plain strings.Builder
	for _, literal := range literals {
		before, after, _ := strings.Cut(line, "\x00")
		plain.WriteString(before)
		plain.WriteString(literal)
		line = after
	}
	plain.WriteString(line)
	return plain.String()
}

// Match delimiter runs instead of independent regex spans, so nested
// emphasis can share a closing run without leaving or reordering markers.
func stripMarkdownEmphasis(text string) string {
	type opener struct {
		start, remaining int
		canClose         bool
	}
	runes := []rune(text)
	removed := make([]bool, len(runes))
	var openers [2][]opener
	for i := 0; i < len(runes); {
		marker := runes[i]
		if marker != '*' && marker != '_' {
			i++
			continue
		}
		start := i
		for i < len(runes) && runes[i] == marker {
			i++
		}
		count := i - start
		before, after := '\n', '\n'
		if start > 0 {
			before = runes[start-1]
		}
		if i < len(runes) {
			after = runes[i]
		}
		beforePunct := unicode.IsPunct(before) || unicode.IsSymbol(before)
		afterPunct := unicode.IsPunct(after) || unicode.IsSymbol(after)
		canOpen := !unicode.IsSpace(after) && (!afterPunct || unicode.IsSpace(before) || beforePunct)
		canClose := !unicode.IsSpace(before) && (!beforePunct || unicode.IsSpace(after) || afterPunct)
		kind := 0
		if marker == '_' {
			kind = 1
			canOpen, canClose = canOpen && (!canClose || beforePunct), canClose && (!canOpen || afterPunct)
		}
		remaining := count
		for canClose && remaining > 0 {
			candidate := len(openers[kind]) - 1
			for candidate >= 0 {
				open := openers[kind][candidate]
				// CommonMark's rule of three disambiguates runs that can
				// both open and close emphasis.
				if !(open.canClose || canOpen) || (open.remaining+remaining)%3 != 0 ||
					(open.remaining%3 == 0 && remaining%3 == 0) {
					break
				}
				candidate--
			}
			if candidate < 0 {
				break
			}
			openers[kind] = openers[kind][:candidate+1]
			open := &openers[kind][candidate]
			for len(openers[1-kind]) > 0 && openers[1-kind][len(openers[1-kind])-1].start > open.start {
				openers[1-kind] = openers[1-kind][:len(openers[1-kind])-1]
			}
			used := min(open.remaining, remaining)
			for j := range used {
				removed[open.start+open.remaining-used+j] = true
				removed[start+count-remaining+j] = true
			}
			open.remaining -= used
			remaining -= used
			if open.remaining == 0 {
				openers[kind] = openers[kind][:candidate]
			}
		}
		if canOpen && remaining > 0 {
			openers[kind] = append(openers[kind], opener{start: start + count - remaining, remaining: remaining, canClose: canClose})
		}
	}
	var out strings.Builder
	for i, r := range runes {
		if !removed[i] {
			out.WriteRune(r)
		}
	}
	return out.String()
}
