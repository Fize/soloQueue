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
	"的": true, "了": true, "在": true, "是": true, "我": true, "有": true,
	"和": true, "就": true, "不": true, "人": true, "都": true, "一": true,
	"一个": true, "上": true, "也": true, "很": true, "到": true, "说": true,
	"要": true, "去": true, "你": true, "会": true, "着": true, "没有": true,
	"看": true, "好": true, "自己": true, "这": true, "他": true, "她": true,
	"它": true, "们": true, "那": true, "什么": true, "怎么": true,
	"为什么": true, "因为": true, "所以": true, "但是": true, "如果": true,
	"虽然": true, "而且": true, "或者": true, "然后": true, "可以": true,
	"应该": true, "能": true, "能够": true, "可能": true, "已经": true,
	"还": true, "又": true, "再": true, "更": true, "最": true, "把": true,
	"被": true, "让": true, "给": true, "对": true, "从": true, "向": true,
	"跟": true, "与": true, "及": true, "等等": true, "等": true, "吗": true,
	"呢": true, "啊": true, "吧": true, "嗯": true, "哦": true, "哈": true,
}

func isStopword(word string) bool {
	return englishStopwords[word] || chineseStopwords[word]
}
