package router

import (
	_ "embed"
	"encoding/json"
)

//go:embed language/patterns.json
var routerLanguageJSON []byte

var routerLanguage = func() struct {
	Fasttrack struct{ Research, General, Engineering, EngineeringEntity, EngineeringAction, EngineeringDefect string }
} {
	var data struct {
		Fasttrack struct{ Research, General, Engineering, EngineeringEntity, EngineeringAction, EngineeringDefect string }
	}
	if err := json.Unmarshal(routerLanguageJSON, &data); err != nil {
		panic(err)
	}
	return data
}()
