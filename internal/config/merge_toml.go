package config

import (
	"fmt"

	"github.com/pelletier/go-toml/v2"
)

// MergeTOML 将 src TOML bytes 部分覆盖合并到 dst，返回新值
//
// 合并策略：
//   - 对象字段：逐一覆盖（partial merge，未指定字段保留上层值）
//   - 数组字段：整体替换（非追加）
//   - null / 缺失字段：跳过，保留上层值
//
// 实现方式：将 dst 序列化为 TOML，再将 src 解析为 map，
// 递归合并后反序列化回 T。
func MergeTOML[T any](dst T, src []byte) (T, error) {
	// 将 dst 序列化为通用 map
	dstBytes, err := toml.Marshal(dst)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("MergeTOML: marshal dst: %w", err)
	}

	var dstMap map[string]any
	if err := toml.Unmarshal(dstBytes, &dstMap); err != nil {
		var zero T
		return zero, fmt.Errorf("MergeTOML: unmarshal dst map: %w", err)
	}

	// 解析 src
	var srcMap map[string]any
	if err := toml.Unmarshal(src, &srcMap); err != nil {
		var zero T
		return zero, fmt.Errorf("MergeTOML: unmarshal src: %w", err)
	}

	// 递归合并
	mergeMapTOML(dstMap, srcMap)

	// 序列化合并结果，反序列化回 T
	merged, err := toml.Marshal(dstMap)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("MergeTOML: marshal merged: %w", err)
	}

	var result T
	if err := toml.Unmarshal(merged, &result); err != nil {
		var zero T
		return zero, fmt.Errorf("MergeTOML: unmarshal result: %w", err)
	}

	return result, nil
}

// mergeMapTOML 递归合并 src 到 dst（原地修改 dst）
//
// 规则：
//   - src 中的对象字段递归合并到 dst 对应字段
//   - src 中的数组字段整体替换 dst 对应字段
//   - src 中的 null 值跳过（保留 dst 原值）
//   - 其他类型（string/number/bool）直接覆盖
func mergeMapTOML(dst, src map[string]any) {
	for k, srcVal := range src {
		// null 跳过
		if srcVal == nil {
			continue
		}

		srcObj, srcIsObj := srcVal.(map[string]any)
		dstVal, exists := dst[k]

		if srcIsObj && exists {
			dstObj, dstIsObj := dstVal.(map[string]any)
			if dstIsObj {
				// 两边都是对象：递归合并
				mergeMapTOML(dstObj, srcObj)
				continue
			}
		}

		// 数组、标量、新键：直接覆盖
		dst[k] = srcVal
	}
}
