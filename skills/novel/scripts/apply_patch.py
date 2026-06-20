#!/usr/bin/env python3
# -*- coding: utf-8 -*-

import sys
import os
import re

def parse_patch(patch_str):
    # 匹配如下结构：
    # <<<< 10-15
    # 原文内容
    # ====
    # 新替换内容
    # >>>>
    pattern = re.compile(r"<<<<\s*(\d+)-(\d+)\s*\n(.*?)\n====\n(.*?)\n>>>>", re.DOTALL)
    matches = pattern.findall(patch_str)
    
    blocks = []
    for match in matches:
        start_line = int(match[0])
        end_line = int(match[1])
        orig_content = match[2]
        new_content = match[3]
        blocks.append({
            "start": start_line,
            "end": end_line,
            "orig": orig_content,
            "new": new_content
        })
    return blocks

def apply_patch(file_path, patch_str):
    if not os.path.exists(file_path):
        print(f"错误: 找不到目标文件: {file_path}")
        return False
        
    with open(file_path, "r", encoding="utf-8", errors="ignore") as f:
        lines = f.readlines()
        
    blocks = parse_patch(patch_str)
    if not blocks:
        print("警告: 未在输入中解析到格式为 <<<< start-end ... ==== ... >>>> 的补丁块。")
        return False
        
    # 从后往前应用，避免因为行数增减导致前部行号偏移失效
    blocks.sort(key=lambda x: x["start"], reverse=True)
    
    success = True
    for block in blocks:
        start_idx = block["start"] - 1
        end_idx = block["end"] - 1 # 包含结尾行
        
        if start_idx < 0 or end_idx >= len(lines) or start_idx > end_idx:
            print(f"错误: 补丁范围 {block['start']}-{block['end']} 超出文件实际行数范围（当前文件共 {len(lines)} 行）")
            success = False
            continue
            
        # 验证原始内容匹配度 (容忍度对比)
        expected_orig = block["orig"].strip()
        actual_orig = "".join(lines[start_idx:end_idx+1]).strip()
        
        expected_clean = re.sub(r"\s+", "", expected_orig)
        actual_clean = re.sub(r"\s+", "", actual_orig)
        
        if expected_clean != actual_clean:
            print(f"提示: 第 {block['start']}-{block['end']} 行内容与预期内容不完全一致。")
            print(f"预期 (补丁中): {repr(expected_orig)}")
            print(f"实际 (文件中): {repr(actual_orig)}")
            
        # 分离新内容为多行，确保行尾换行符一致
        new_lines = block["new"].splitlines(keepends=True)
        new_lines_formatted = []
        for nl in new_lines:
            if not nl.endswith("\n"):
                new_lines_formatted.append(nl + "\n")
            else:
                new_lines_formatted.append(nl)
                
        # 执行行替换
        lines[start_idx:end_idx+1] = new_lines_formatted
        print(f"成功应用补丁到第 {block['start']}-{block['end']} 行")
        
    if success:
        with open(file_path, "w", encoding="utf-8") as f:
            f.writelines(lines)
        return True
    return False

def main():
    if len(sys.argv) < 3:
        print("用法: python apply_patch.py <目标正文路径> <补丁文件或补丁文本内容>")
        sys.exit(1)
        
    file_path = sys.argv[1]
    patch_source = sys.argv[2]
    
    if os.path.exists(patch_source):
        with open(patch_source, "r", encoding="utf-8", errors="ignore") as f:
            patch_content = f.read()
    else:
        patch_content = patch_source
        
    if apply_patch(file_path, patch_content):
        print("所有补丁成功合并！")
        sys.exit(0)
    else:
        print("部分或全部补丁合并失败。")
        sys.exit(1)

if __name__ == "__main__":
    main()
