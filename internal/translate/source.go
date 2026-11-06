package translate

import (
	"regexp"
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

// Match literals and URLs alongside comments so their contents survive.
const codeLiterals = `[[:alpha:]][[:alnum:]+.\-]*://[^\s"'<>]+` +
	`|"""(?s:.*?)"""|'''(?s:.*?)'''|"(?:\\[^\n]|[^"\\\n])*"|'(?:\\[^\n]|[^'\\\n])*'|` +
	"``[^\n]+?``|`[^`]+`"

const slashComment = `//[^\n]*|/\*(?:[^*]|\*+[^*/])*\*+/`
const mixedHashComment = `^[ \t]*#(?:[[:alpha:]_#!]|[ \t]+)[^\n]*|[ \t]+#[ \t]+[^\n]*`

// Untagged snippets can contain Python floor division. Keep clearly numeric
// expressions while still recognizing slash comments on their own line.
const numericFloorDivision = `(?:\b[[:alnum:]_]+|[)\]])[ \t]*//[ \t]*[+-]?[0-9]+(?:\.[0-9]+)?`

var commentTokens = [...]*regexp.Regexp{
	mixedComments:  regexp.MustCompile("(?m)" + codeLiterals + "|" + numericFloorDivision + "|" + slashComment + "|" + mixedHashComment),
	slashComments:  regexp.MustCompile("(?m)" + codeLiterals + "|" + slashComment),
	hashComments:   regexp.MustCompile("(?m)" + codeLiterals + `|(?:^|[ \t]+)#[^\n]*`),
	pythonComments: regexp.MustCompile("(?m)" + codeLiterals + `|[ \t]*#[^\n]*`),
}

// A blank line after a hash heading separates it from a following example.
const codePrefix = `^[ \t]*(?:\n[ \t]*)*(?:#[^\n]*\n[ \t]*)*`

var (
	codeDirective  = regexp.MustCompile(`^#[ \t]*(?:define|elif|else|endif|error|if|ifdef|ifndef|include|line|pragma|undef|warning)\b`)
	pythonSource   = regexp.MustCompile(`(?m)^[ \t]*(?:async[ \t]+def[ \t]+|def[ \t]+\w+[ \t]*\(|class[ \t]+\w+[^\n]*:|from[ \t]+[\w.]+[ \t]+import[ \t]+|import[ \t]+[\w.]+[ \t]*(?:#.*)?$|print[ \t]*\()`)
	codeAssignment = regexp.MustCompile(codePrefix + `[[:alpha:]_]\w*[ \t]*=[ \t]*[^=\s]`)
	codePreamble   = regexp.MustCompile(codePrefix + `$`)
)

func stripCodeComments(text string, style commentStyle, preserveHeadings bool) string {
	var out strings.Builder
	end := 0
	for _, match := range commentTokens[style].FindAllStringIndex(text, -1) {
		start := match[0]
		out.WriteString(text[end:start])
		end = match[1]
		token := text[start:end]
		marker := strings.TrimLeft(token, " \t")
		switch {
		case strings.HasPrefix(token, "//"):
		case strings.HasPrefix(token, "/*"):
			// Keep line structure and avoid joining adjacent tokens.
			if n := strings.Count(token, "\n"); n > 0 {
				out.WriteString(strings.Repeat("\n", n))
			} else {
				out.WriteByte(' ')
			}
		case strings.HasPrefix(marker, "#"):
			lineStart := strings.LastIndexByte(text[:start], '\n') + 1
			if strings.HasPrefix(marker, "#!") || (style == mixedComments && codeDirective.MatchString(marker)) ||
				(preserveHeadings && markdownHeading.MatchString(markdownQuote.ReplaceAllString(text[lineStart:end], ""))) {
				out.WriteString(token)
			}
		default:
			out.WriteString(token)
		}
	}
	out.WriteString(text[end:])
	return out.String()
}

var (
	markdownFence   = regexp.MustCompile("^[ \t]{0,3}(" + "`{3,}|~{3,}" + ")(.*)$")
	markdownQuote   = regexp.MustCompile(`^(?:[ \t]{0,3}>[ \t]?)+`)
	markdownHeading = regexp.MustCompile(`^[ \t]{0,3}#{1,6}[ \t]+(.*?)(?:[ \t]+#+[ \t]*)?$`)
	markdownList    = regexp.MustCompile(`^[ \t]*(?:[-+*]|[0-9]{1,9}[.)])[ \t]+(?:\[[ xX]\][ \t]+)?`)
	markdownRule    = regexp.MustCompile(`^[ \t]{0,3}(?:\*(?:[ \t]*\*){2,}|-(?:[ \t]*-){2,}|_(?:[ \t]*_){2,})[ \t]*$`)
	markdownInline  = regexp.MustCompile("``([^`].*?)``|`([^`]+)`" + `|!?\[([^\[\]]+)\]\((?:<[^<>]+>|(?:[^()\s]|\([^()\s]*\))+)(?:[ \t]+"[^"]*")?\)|\\([[:punct:]])|\x00`)
	markdownStars   = regexp.MustCompile(`\*{3}([^*\n]+)\*{3}|\*{2}([^*\n]+)\*{2}|\*([^*\s][^*\n]*?)\*`)
	markdownUnder   = regexp.MustCompile(`(^|[^\pL\pN_])(?:___([^_\n]+)___|__([^_\n]+)__|_([^_\s][^_\n]*?)_)`)
	markdownStrike  = regexp.MustCompile(`~~([^~\n]+)~~`)
)

// A language tag scopes comment syntax inside its fence. Outside fences,
// prefer the Markdown reading of a heading unless the block looks like Python.
func cleanSourceBlocks(text string, options config.IdentifierSettings) string {
	lines := strings.Split(text, "\n")
	var out []string
	var fence string
	style := mixedComments
	start := 0
	writeBlock := func(end int) {
		if start == end {
			return
		}
		block := strings.Join(lines[start:end], "\n")
		blockStyle := style
		plainCode := false
		if options.StripCodeComments {
			startsWithAssignment := fence == "" && codeAssignment.MatchString(block)
			if blockStyle == mixedComments {
				match := pythonSource.FindStringIndex(block)
				if match != nil && (fence != "" || startsWithAssignment || codePreamble.MatchString(block[:match[0]])) {
					blockStyle = pythonComments
				}
			}
			plainCode = fence == "" && (blockStyle == pythonComments || startsWithAssignment)
			block = stripCodeComments(block, blockStyle, options.StripMarkdown && fence == "" && !plainCode)
		}
		if options.StripMarkdown && fence == "" && !plainCode {
			block = stripMarkdown(block)
		}
		out = append(out, block)
	}
	for i, line := range lines {
		match := markdownFence.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if fence != "" && (!strings.HasPrefix(match[1], fence) || strings.TrimSpace(match[2]) != "") {
			continue
		}
		writeBlock(i)
		if !options.StripMarkdown {
			out = append(out, line)
		}
		if fence == "" {
			fence = match[1]
			tag := strings.ToLower(strings.TrimSpace(match[2]))
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
		start = i + 1
	}
	writeBlock(len(lines))
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
	line = markdownStars.ReplaceAllString(masked.String(), "$1$2$3")
	line = stripMarkdownUnderscores(line)
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

func stripMarkdownUnderscores(line string) string {
	// Inspect the closing boundary without consuming it. It may be the
	// opening boundary of the next emphasis span.
	var out strings.Builder
	start := 0
	for _, m := range markdownUnder.FindAllStringSubmatchIndex(line, -1) {
		next, _ := utf8.DecodeRuneInString(line[m[1]:])
		if next == '_' || unicode.IsLetter(next) || unicode.IsNumber(next) {
			continue
		}
		out.WriteString(line[start:m[3]])
		for i := 4; i < len(m); i += 2 {
			if m[i] >= 0 {
				out.WriteString(line[m[i]:m[i+1]])
			}
		}
		start = m[1]
	}
	out.WriteString(line[start:])
	return out.String()
}
