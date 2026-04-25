package main

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/pterm/pterm"
)

func main() {
	// 1. 定义视觉样式 (Lip Gloss)
	// 模仿 Claude Code 的侧边栏样式
	var agentStyle = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true). // 仅左边框
		BorderForeground(lipgloss.Color("63")).                     // 紫色边框
		PaddingLeft(2).
		MarginBottom(1)

	var userStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")).
		Bold(true)

	// 2. 模拟用户输入
	fmt.Println(userStyle.Render("❯ ") + "帮我分析一下 bj-pub-pd 里的 Pod 状态")

	// 3. 使用 pterm.DefaultArea 实现 Inline 流式渲染
	// Area 可以在不全屏的情况下，局部刷新指定的行
	area, _ := pterm.DefaultArea.Start()
	
	fullResponse := ""
	mockChunks := []string{
		"正在连接 Kubernetes 集群...",
		"\n发现 3 个异常 Pod：",
		"\n - \033[31morder-api-v2\033[0m (CrashLoopBackOff)",
		"\n - \033[31mpayment-worker\033[0m (OOMKilled)",
		"\n原因分析：节点内存不足。建议执行 SoloQueue 优化脚本。",
	}

	for _, chunk := range mockChunks {
		fullResponse += chunk
		// 使用 Lip Gloss 渲染样式，然后交给 Area 更新
		area.Update(agentStyle.Render("Agent:\n" + fullResponse))
		time.Sleep(300 * time.Millisecond)
	}
	
	area.Stop() // 停止后，内容就固定在终端里了

	// 4. 下一次输入依然在下方进行
	fmt.Println(userStyle.Render("❯ ") + "执行优化")
}