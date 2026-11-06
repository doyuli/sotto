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
		{"line and block comments", "a/* comment */b\n// comment\nc # comment\n# another comment", "a b\n\nc", comments},
		{"multiline block comment", "a/* first\nsecond */b", "a\nb", comments},
		{"keep code directives", "#include <stdio.h>\n# another comment\nint main() {}", "#include <stdio.h>\n\nint main() {}", comments},
		{"strings and URL", "url = \"https://example.test/a//b#c\" // remove\nhttps://example.test/a//b#c", "url = \"https://example.test/a//b#c\" \nhttps://example.test/a//b#c", comments},
		{"hash reference in prose", "Ticket #123 was fixed\n#456 still open", "Ticket #123 was fixed\n#456 still open", comments},
		{"untagged floor division", "value = 12 // 3", "value = 12 // 3", comments},
		{"tagged Python comment syntax", "```python\n# keep floor division\nvalue = total // count # comment\n```", "```python\n\nvalue = total // count\n```", comments},
		{"tagged Go comment syntax", "```go\nx := 1 // 2024 note\n```", "```go\nx := 1 \n```", comments},
		{"tagged shell parameter syntax", "```sh\necho $# ${name#prefix} # comment\n```", "echo $# ${name#prefix}", formatAndComments},
		{"triple quoted Python literal", "help = \"\"\"first line\n# literal\nlast line\"\"\" # comment", "help = \"\"\"first line\n# literal\nlast line\"\"\"", formatAndComments},
		{"line endings", "one  \r\n \r\n\ttwo\r三\u2028四", "one two 三 四", newlines},
		{"markdown", "# Heading\n> **Bold** and _italic_\n- [x] [A link](https://example.test)\n1. `user_id`\n---", "Heading\nBold and italic\nA link\nuser_id", markdown},
		{"code remains literal", "```sh\n# comment\na='*literal*'\n```", "# comment\na='*literal*'", markdown},
		{"emphasis around code and links", "**Use `user_id`** and **[the docs](https://example.test)**", "Use user_id and the docs", markdown},
		{"adjacent underscore emphasis", "_one_ _two_ __three__ ___four___ user_id", "one two three four user_id", markdown},
		{"escaped and literal emphasis", "\\*literal\\* and **`*literal*`**", "*literal* and *literal*", markdown},
		{"inline comment markers remain literal", "Use `//` for line comments and `# heading` as an example", "Use // for line comments and # heading as an example", formatAndComments},
		{"standalone Python comment with Markdown on", "# standalone Python comment\nprint(1)", "print(1)", formatAndComments},
		{"standalone hash comment before assignment", "# keep this private\nvalue = 1", "value = 1", formatAndComments},
		{"headings with comments on", "# Heading\n> ## Quoted heading\nParagraph", "Heading\nQuoted heading\nParagraph", formatAndComments},
		{"prose before untagged Python example", "# Heading\n\nTry this.\n\nprint(1)", "Heading\n\nTry this.\n\nprint(1)", formatAndComments},
		{"combined cleanup", "# Heading\n- **Item**\n```sh\n# Comment\necho hi # comment\n```", "Heading Item echo hi", all},
		{"only comments", "// comment\n/* block */", "", comments},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PrepareSource(tt.input, tt.options); got != tt.want {
				t.Errorf("PrepareSource(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
