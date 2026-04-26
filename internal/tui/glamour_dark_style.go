package tui

// glamour_dark_style.go — custom dark JSON style for glamour markdown rendering.
//
// This works around a bug in glamour v1/v2 where the built-in "dark" and "light"
// standard styles fail to render H2-H4 headings (they appear as literal "##" text).
// The style definitions are based on glamour's dark.json with all heading levels
// explicitly defined.

const darkStyleJSON = `{
	"block_quote": {
		"indent": 1,
		"indent_token": "│"
	},
	"paragraph": {},
	"list": {
		"color": "252"
	},
	"heading": {
		"bold": true
	},
	"h1": {
		"prefix": " ",
		"color": "#b39afc",
		"bold": true
	},
	"h2": {
		"prefix": " ",
		"color": "#b39afc",
		"bold": true
	},
	"h3": {
		"prefix": " ",
		"color": "#b39afc",
		"bold": true
	},
	"h4": {
		"prefix": " ",
		"color": "#b39afc",
		"bold": true
	},
	"h5": {
		"prefix": " ",
		"color": "#b39afc",
		"bold": true
	},
	"h6": {
		"prefix": " ",
		"color": "#b39afc",
		"bold": true
	},
	"text": {},
	"strong": {
		"bold": true
	},
	"em": {
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
		"theme": "dracula",
		"chroma": {
			"text": {
				"color": "#f8f8f2"
			},
			"error": {
				"color": "#f8f8f2",
				"background_color": "#ff5555"
			},
			"keyword": {
				"color": "#ff79c6"
			},
			"keyword.namespace": {
				"color": "#ff79c6"
			},
			"keyword.type": {
				"color": "#8be9fd"
			},
			"keyword.constant": {
				"color": "#bd93f9"
			},
			"name": {
				"color": "#f8f8f2"
			},
			"name.builtin": {
				"color": "#8be9fd"
			},
			"name.class": {
				"color": "#8be9fd"
			},
			"name.function": {
				"color": "#50fa7b"
			},
			"name.function.macro": {
				"color": "#50fa7b",
				"italic": true
			},
			"literal": {
				"color": "#bd93f9"
			},
			"literal.string": {
				"color": "#f1fa8c"
			},
			"literal.string.doc": {
				"color": "#6272a4"
			},
			"comment": {
				"color": "#6272a4",
				"italic": true
			},
			"operator": {
				"color": "#ff79c6"
			},
			"punctuation": {
				"color": "#f8f8f2"
			},
			"generic": {
				"color": "#bd93f9"
			}
		}
	},
	"code": {
		"color": "#ff79c6",
		"background_color": "#2d2d2d"
	}
}`

const lightStyleJSON = `{
	"block_quote": {
		"indent": 1,
		"indent_token": "│"
	},
	"paragraph": {},
	"list": {},
	"heading": {
		"bold": true
	},
	"h1": {
		"prefix": " ",
		"color": "#7c3aed",
		"bold": true
	},
	"h2": {
		"prefix": " ",
		"color": "#7c3aed",
		"bold": true
	},
	"h3": {
		"prefix": " ",
		"color": "#7c3aed",
		"bold": true
	},
	"h4": {
		"prefix": " ",
		"color": "#7c3aed",
		"bold": true
	},
	"h5": {
		"prefix": " ",
		"color": "#7c3aed",
		"bold": true
	},
	"h6": {
		"prefix": " ",
		"color": "#7c3aed",
		"bold": true
	},
	"text": {},
	"strong": {
		"bold": true
	},
	"em": {
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
