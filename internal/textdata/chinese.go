// Package textdata provides embedded language-specific application text.
package textdata

import (
	_ "embed"
	"encoding/json"
)

//go:embed zh.json
var chineseJSON []byte

var chineseText = func() map[string]string {
	var values map[string]string
	if err := json.Unmarshal(chineseJSON, &values); err != nil {
		panic(err)
	}
	return values
}()

// Chinese returns a named Chinese string used by language-specific logic.
func Chinese(key string) string {
	value, ok := chineseText[key]
	if !ok {
		panic("missing Chinese text: " + key)
	}
	return value
}
