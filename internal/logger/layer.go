package logger

// Layer 定义日志层级，对应不同的生命周期范围
type Layer string

const (
	LayerSystem  Layer = "system"
	LayerTeam    Layer = "team"
	LayerSession Layer = "session"
)

// Category 定义日志分类，每个 Category 与特定 Layer 绑定
type Category string

const (
	// system layer
	CatApp    Category = "app"
	CatConfig Category = "config"
	CatHTTP   Category = "http"
	CatWS     Category = "ws"

	// team layer
	CatTeam  Category = "team"
	CatAgent Category = "agent"

	// session layer
	CatLLM      Category = "llm"
	CatActor    Category = "actor"
	CatTool     Category = "tool"
	CatMessages Category = "messages"
)

// layerCategories 约束合法的 layer+category 组合
//
// CatLLM 同时属于 system 层（deepseek client 内部日志）和 session 层（agent 内部日志）
var layerCategories = map[Layer][]Category{
	LayerSystem:  {CatApp, CatConfig, CatHTTP, CatWS, CatLLM},
	LayerTeam:    {CatTeam, CatAgent},
	LayerSession: {CatLLM, CatApp, CatActor, CatTool, CatMessages},
}

// categoryPrimaryLayer 定义 Category 的首选 layer
// 当一个 Category 属于多个 layer 时，LayerForCategory 返回首选层
var categoryPrimaryLayer = map[Category]Layer{
	CatApp:      LayerSystem,
	CatConfig:   LayerSystem,
	CatHTTP:     LayerSystem,
	CatWS:       LayerSystem,
	CatLLM:      LayerSession, // session 层优先（agent 内部日志）
	CatTeam:     LayerTeam,
	CatAgent:    LayerTeam,
	CatActor:    LayerSession,
	CatTool:     LayerSession,
	CatMessages: LayerSession,
}

// ValidCategory 检查 category 是否属于给定 layer
func ValidCategory(layer Layer, cat Category) bool {
	cats, ok := layerCategories[layer]
	if !ok {
		return false
	}
	for _, c := range cats {
		if c == cat {
			return true
		}
	}
	return false
}

// LayerForCategory 根据 category 反查所属 layer
//
// 当 Category 属于多个 layer 时，返回首选层（由 categoryPrimaryLayer 定义）
func LayerForCategory(cat Category) (Layer, bool) {
	lay, ok := categoryPrimaryLayer[cat]
	return lay, ok
}
