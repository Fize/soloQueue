package config

import (
	"encoding/json"
	"fmt"
)

// MergeJSON 将 src JSON bytes 部分覆盖合并到 dst，返回新值
//
// 合并策略：
//   - 对象字段：逐一覆盖（partial merge，未指定字段保留上层值）
//   - 数组字段：整体替换（非追加）
//   - null / 缺失字段：跳过，保留上层值
//
// 实现方式：将 dst 序列化为 map，再将 src 解析为 map，
// 递归合并后反序列化回 T。
func MergeJSON[T any](dst T, src []byte) (T, error) {
	// 将 dst 序列化为通用 map
	dstBytes, err := json.Marshal(dst)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("MergeJSON: marshal dst: %w", err)
	}

	var dstMap map[string]any
	if err := json.Unmarshal(dstBytes, &dstMap); err != nil {
		var zero T
		return zero, fmt.Errorf("MergeJSON: unmarshal dst map: %w", err)
	}

	// 解析 src
	var srcMap map[string]any
	if err := json.Unmarshal(src, &srcMap); err != nil {
		var zero T
		return zero, fmt.Errorf("MergeJSON: unmarshal src: %w", err)
	}

	// 递归合并
	mergeMap(dstMap, srcMap)

	// 序列化合并结果，反序列化回 T
	merged, err := json.Marshal(dstMap)
	if err != nil {
		var zero T
		return zero, fmt.Errorf("MergeJSON: marshal merged: %w", err)
	}

	var result T
	if err := json.Unmarshal(merged, &result); err != nil {
		var zero T
		return zero, fmt.Errorf("MergeJSON: unmarshal result: %w", err)
	}

	return result, nil
}

// mergeMap 递归合并 src 到 dst（原地修改 dst）
//
// 规则：
//   - src 中的对象字段递归合并到 dst 对应字段
//   - src 中的数组字段整体替换 dst 对应字段
//   - src 中的 null 值跳过（保留 dst 原值）
//   - 其他类型（string/number/bool）直接覆盖
func mergeMap(dst, src map[string]any) {
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
				mergeMap(dstObj, srcObj)
				continue
			}
		}

		// 数组、标量、新键：直接覆盖
		dst[k] = srcVal
	}
}
