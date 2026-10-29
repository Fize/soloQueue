// Package testfixtures provides shared multilingual samples for tests.
package testfixtures

import (
	_ "embed"
	"encoding/json"
)

//go:embed chinese_samples.json
var chineseJSON []byte

var chineseSamples = func() map[string]string {
	var samples map[string]string
	if err := json.Unmarshal(chineseJSON, &samples); err != nil {
		panic(err)
	}
	return samples
}()

// Chinese returns a named sample and fails fast if its key is missing.
func Chinese(key string) string {
	value, ok := chineseSamples[key]
	if !ok {
		panic("missing Chinese test sample: " + key)
	}
	return value
}
