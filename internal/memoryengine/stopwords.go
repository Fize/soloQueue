package memoryengine

var englishStopwords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "but": true,
	"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
	"with": true, "by": true, "from": true, "is": true, "are": true, "was": true,
	"were": true, "be": true, "been": true, "being": true, "have": true,
	"has": true, "had": true, "do": true, "does": true, "did": true,
	"will": true, "would": true, "could": true, "should": true, "may": true,
	"might": true, "shall": true, "can": true, "need": true, "this": true,
	"that": true, "these": true, "those": true, "i": true, "you": true,
	"he": true, "she": true, "it": true, "we": true, "they": true, "me": true,
	"him": true, "her": true, "us": true, "them": true, "my": true, "your": true,
	"his": true, "its": true, "our": true, "their": true, "what": true,
	"which": true, "who": true, "whom": true, "when": true, "where": true,
	"why": true, "how": true, "all": true, "each": true, "every": true,
	"both": true, "few": true, "more": true, "most": true, "other": true,
	"some": true, "such": true, "no": true, "nor": true, "not": true,
	"only": true, "own": true, "same": true, "so": true, "than": true,
	"too": true, "very": true, "just": true, "because": true, "as": true,
	"until": true, "while": true, "about": true, "between": true,
	"through": true, "during": true, "before": true, "after": true,
	"above": true, "below": true, "over": true, "under": true, "again": true,
	"further": true, "then": true, "once": true, "here": true, "there": true,
	"also": true, "well": true, "if": true, "into": true, "off": true,
	"up": true, "down": true, "out": true,
}

var chineseStopwords = map[string]bool{
	// Map previously used for Chinese stopwords, emptied for English-only mode
}

func isStopword(word string) bool {
	return englishStopwords[word] || chineseStopwords[word]
}