package tui

// glamour_dark_style.go — custom dark JSON style for glamour markdown rendering.
//
// Glamour v2's built-in "dark" style renders heading text with ANSI 256-color
// codes that can break in some terminals. This custom style uses true-color hex
// codes for headings and includes the document block needed for correct layout.

const darkStyleJSON = `{
	"document": {
		"block_prefix": "\n",
		"block_suffix": "\n"
	},
	"block_quote": {
		"indent": 1,
		"indent_token": "│ "
	},
	"paragraph": {},
	"list": {
		"level_indent": 2
	},
	"heading": {
		"block_suffix": "\n",
		"color": "#bd93f9",
		"bold": true
	},
	"h1": {
		"prefix": "# ",
		"color": "#b39afc",
		"bold": true
	},
	"h2": {
		"prefix": "## ",
		"color": "#b39afc",
		"bold": true
	},
	"h3": {
		"prefix": "### ",
		"color": "#b39afc",
		"bold": true
	},
	"h4": {
		"prefix": "#### ",
		"color": "#b39afc",
		"bold": true
	},
	"h5": {
		"prefix": "##### ",
		"color": "#b39afc",
		"bold": true
	},
	"h6": {
		"prefix": "###### ",
		"color": "#b39afc",
		"bold": true
	},
	"text": {},
	"strong": {
		"bold": true
	},
	"emph": {
		"italic": true
	},
	"strikethrough": {
		"crossed_out": true
	},
	"link": {
		"color": "#6ba6f7",
		"underline": true
	},
	"code_block": {
		"theme": "dracula"
	},
	"code": {
		"color": "#ff79c6",
		"background_color": "#2d2d2d"
	}
}`

const lightStyleJSON = `{
	"document": {
		"block_prefix": "\n",
		"block_suffix": "\n"
	},
	"block_quote": {
		"indent": 1,
		"indent_token": "│ "
	},
	"paragraph": {},
	"list": {
		"level_indent": 2
	},
	"heading": {
		"block_suffix": "\n",
		"color": "#7c3aed",
		"bold": true
	},
	"h1": {
		"prefix": "# ",
		"color": "#7c3aed",
		"bold": true
	},
	"h2": {
		"prefix": "## ",
		"color": "#7c3aed",
		"bold": true
	},
	"h3": {
		"prefix": "### ",
		"color": "#7c3aed",
		"bold": true
	},
	"h4": {
		"prefix": "#### ",
		"color": "#7c3aed",
		"bold": true
	},
	"h5": {
		"prefix": "##### ",
		"color": "#7c3aed",
		"bold": true
	},
	"h6": {
		"prefix": "###### ",
		"color": "#7c3aed",
		"bold": true
	},
	"text": {},
	"strong": {
		"bold": true
	},
	"emph": {
		"italic": true
	},
	"strikethrough": {
		"crossed_out": true
	},
	"link": {
		"color": "#2563eb",
		"underline": true
	},
	"code_block": {
		"theme": "pygments"
	},
	"code": {
		"color": "#d63384"
	}
}`
