package logger

// Category 定义日志分类
type Category string

const (
	CatApp      Category = "app"
	CatConfig   Category = "config"
	CatHTTP     Category = "http"
	CatWS       Category = "ws"
	CatLLM      Category = "llm"
	CatTeam     Category = "team"
	CatAgent    Category = "agent"
	CatActor    Category = "actor"
	CatTool     Category = "tool"
	CatMessages Category = "messages"
	CatMCP      Category = "mcp"
)

// systemCategories 定义所有合法的日志分类（统一写入 system 目录）
var systemCategories = []Category{
	CatApp, CatConfig, CatHTTP, CatWS, CatLLM,
	CatTeam, CatAgent,
	CatActor, CatTool, CatMessages, CatMCP,
}

// ValidCategory 检查 category 是否合法
func ValidCategory(cat Category) bool {
	for _, c := range systemCategories {
		if c == cat {
			return true
		}
	}
	return false
}
