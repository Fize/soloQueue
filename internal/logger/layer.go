package logger

// Category defines log categories
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
	CatSimulation Category = "simulation"
)

// systemCategories defines all valid log categories (all written to the 'system' directory)
var systemCategories = []Category{
	CatApp, CatConfig, CatHTTP, CatWS, CatLLM,
	CatTeam, CatAgent,
	CatActor, CatTool, CatMessages, CatMCP, CatSimulation,
}

// ValidCategory checks if the category is valid
func ValidCategory(cat Category) bool {
	for _, c := range systemCategories {
		if c == cat {
			return true
		}
	}
	return false
}