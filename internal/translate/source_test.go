package translate

import (
	"testing"

	"sotto/internal/config"
)

func TestPrepareSource(t *testing.T) {
	comments := config.IdentifierSettings{StripCodeComments: true}
	markdown := config.IdentifierSettings{StripMarkdown: true}
	newlines := config.IdentifierSettings{FlattenNewlines: true}
	formatAndComments := config.IdentifierSettings{StripCodeComments: true, StripMarkdown: true}
	all := config.IdentifierSettings{StripCodeComments: true, StripMarkdown: true, FlattenNewlines: true}
	tests := []struct {
		name, input, want string
		options           config.IdentifierSettings
	}{
		{"defaults preserve input", " # hi\r\n`code`  ", " # hi\r\n`code`  ", config.Default().Identifier},
		{"line and block comments", "a/* comment */b\n// comment\nc # comment\n# another comment", "a comment b\ncomment\nc comment\nanother comment", comments},
		{"multiline block comment", "a/* first\nsecond */b", "a first\nsecond b", comments},
		{"keep code directives", "#include <stdio.h>\n# another comment\nint main() {}", "#include <stdio.h>\nanother comment\nint main() {}", comments},
		{"strings and URL", "url = \"https://example.test/a//b#c\" // remove\nhttps://example.test/a//b#c", "url = \"https://example.test/a//b#c\" remove\nhttps://example.test/a//b#c", comments},
		{"hash reference in prose", "Ticket #123 was fixed\n#456 still open", "Ticket #123 was fixed\n#456 still open", comments},
		{"untagged floor division", "value = 12 // 3", "value = 12 // 3", comments},
		{"tagged Python comment syntax", "```python\n# keep floor division\nvalue = total // count # comment\n```", "```python\nkeep floor division\nvalue = total // count comment\n```", comments},
		{"tagged Go comment syntax", "```go\nx := 1 // 2024 note\n```", "```go\nx := 1 2024 note\n```", comments},
		{"tagged shell parameter syntax", "```sh\necho $# ${name#prefix} # comment\n```", "echo $# ${name#prefix} comment", formatAndComments},
		{"triple quoted Python literal", "help = \"\"\"first line\n# literal\nlast line\"\"\" # comment", "help = \"\"\"first line\n# literal\nlast line\"\"\" comment", formatAndComments},
		{"line endings", "one  \r\n \r\n\ttwo\r三\u2028四", "one two 三 四", newlines},
		{"markdown", "# Heading\n> **Bold** and _italic_\n- [x] [A link](https://example.test)\n1. `user_id`\n---", "Heading\nBold and italic\nA link\nuser_id", markdown},
		{"code remains literal", "```sh\n# comment\na='*literal*'\n```", "# comment\na='*literal*'", markdown},
		{"emphasis around code and links", "**Use `user_id`** and **[the docs](https://example.test)**", "Use user_id and the docs", markdown},
		{"adjacent underscore emphasis", "_one_ _two_ __three__ ___four___ user_id", "one two three four user_id", markdown},
		{"escaped and literal emphasis", "\\*literal\\* and **`*literal*`**", "*literal* and *literal*", markdown},
		{"inline comment markers remain literal", "Use `//` for line comments and `# heading` as an example", "Use // for line comments and # heading as an example", formatAndComments},
		{"standalone Python comment with Markdown on", "# standalone Python comment\nprint(1)", "standalone Python comment\nprint(1)", formatAndComments},
		{"standalone hash comment before assignment", "# keep this private\nvalue = 1", "keep this private\nvalue = 1", formatAndComments},
		{"headings with comments on", "# Heading\n> ## Quoted heading\nParagraph", "Heading\nQuoted heading\nParagraph", formatAndComments},
		{"prose before untagged Python example", "# Heading\n\nTry this.\n\nprint(1)", "Heading\n\nTry this.\n\nprint(1)", formatAndComments},
		{"combined cleanup", "# Heading\n- **Item**\n```sh\n# Comment\necho hi # comment\n```", "Heading Item Comment echo hi comment", all},
		{"comment text survives", "// comment\n/* block */", "comment\nblock", comments},
		{"line markers", "// hello\n/// summary\n//! important\n# explanation", "hello\nsummary\nimportant\nexplanation", comments},
		{"block documentation markers", "/**\n * Hello\n * World\n */", "Hello\nWorld", comments},
		{"empty comment between code", "a/**/b", "a b", comments},
		{"comment Markdown with both options", "value = 1 # **description**", "value = 1 description", formatAndComments},
		{"fenced comment Markdown", "```go\n// **Description**\nx := 1\n```", "Description\nx := 1", formatAndComments},
		{"assignment still cleans Markdown", "key = **value**", "key = value", formatAndComments},
		{"Python does not disable later Markdown", "print(1)\n\n**Description**", "print(1)\n\nDescription", formatAndComments},
		{"fence inside Python literal", "value = \"\"\"\n```python\n# literal\n```\n\"\"\"", "value = \"\"\"\n```python\n# literal\n```\n\"\"\"", comments},
		{"fence inside Python literal with both options", "value = \"\"\"\n```python\n# literal\n```\n\"\"\"", "value = \"\"\"\n```python\n# literal\n```\n\"\"\"", formatAndComments},
		{"shell multiline quoted literal", "```sh\nprintf \"hello\n# literal\nworld\" # description\n```", "```sh\nprintf \"hello\n# literal\nworld\" description\n```", comments},
		{"JavaScript regex literal", `const re = /https?:\/\//; // description`, `const re = /https?:\/\//; description`, comments},
		{"division followed by comment", "value = total / count // description", "value = total / count description", comments},
		{"hash comment keyword", "# if necessary, retry\n# error handling\n# normal comment", "if necessary, retry\nerror handling\nnormal comment", comments},
		{"Unicode hash comment", "#中文注释\nvalue = 1 #说明", "中文注释\nvalue = 1 说明", comments},
		{"nested star emphasis", "**outer *inner* end**", "outer inner end", markdown},
		{"nested underscore emphasis", "__outer _inner_ end__", "outer inner end", markdown},
		{"empty comment with Markdown", "a/**/b", "a b", formatAndComments},
		{"block comment after assignment", "value = /* description */ 1", "value = description 1", comments},
		{"regex stars remain literal", `const re = /a*b*/; // **description**`, `const re = /a*b*/; description`, formatAndComments},
		{"Go raw string remains literal", "message := `**literal**`\n**description**", "message := `**literal**`\ndescription", formatAndComments},
		{"mixed nested emphasis", "***outer _inner_*** and **Use `*name*`**", "outer inner and Use *name*", markdown},
		{"literal markers cannot collide", "\x00 **bold** `code`", "\x00 bold code", formatAndComments},
		{"nested emphasis shares closing run", "*outer **inner***", "outer inner", markdown},
		{"intraword underscores stay literal", "user_name and 中文_术语_中文", "user_name and 中文_术语_中文", markdown},
		{"unmatched emphasis stays literal", "*unfinished and __unfinished", "*unfinished and __unfinished", markdown},
		{"fenced shell string with fence text", "```sh\nprintf \"first\n```\n# literal\nlast\"\n```", "printf \"first\n```\n# literal\nlast\"", formatAndComments},
		{"quoted Markdown and later real fence", "A \"quote\".\n```go\n// description\n```", "A \"quote\".\ndescription", formatAndComments},
		{"standalone Python docstring", "\"\"\"\n# literal\n**literal**\n\"\"\"", "\"\"\"\n# literal\n**literal**\n\"\"\"", formatAndComments},
		{"standalone regex literal", `/a*b*/`, `/a*b*/`, formatAndComments},
		{"assignment does not hide Markdown links", "key = **[value](https://example.test)**", "key = value", formatAndComments},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PrepareSource(tt.input, tt.options); got != tt.want {
				t.Errorf("PrepareSource(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
