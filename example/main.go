package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/manifoldco/promptui"
)

// Message 定义极简的上下文状态
type Message struct {
	Role    string // "user" 或 "assistant"
	Content string
}

func main() {
	// 简单的开场白，可以使用 ANSI 转义码加点颜色
	fmt.Println("\033[1;36m🚀 SoloQueue Terminal Agent (Inline 模式)\033[0m")
	fmt.Println("\033[90m输入 'exit' 退出。支持多轮对话。\033[0m\n")

	// 维护对话状态
	var history []Message

	for {
		// 1. 使用 Promptui 构建输入阶段
		prompt := promptui.Prompt{
			Label: "You",
			// 自定义模板，去掉默认的问号，改成类似 Claude Code 的箭头符号
			Templates: &promptui.PromptTemplates{
				Prompt:  "{{ . | bold | cyan }} {{ \"❯\" | bold | cyan }} ",
				Success: "{{ . | bold | cyan }} {{ \"❯\" | bold | cyan }} ",
			},
		}

		// 阻塞等待用户输入
		result, err := prompt.Run()
		if err != nil {
			// 处理 Ctrl+C 或 Ctrl+D
			fmt.Printf("\n\033[31m[终止会话]\033[0m\n")
			break
		}

		input := strings.TrimSpace(result)
		if input == "exit" || input == "quit" {
			fmt.Println("Bye!")
			break
		}
		if input == "" {
			continue // 忽略空输入
		}

		// 记录用户状态
		history = append(history, Message{Role: "user", Content: input})

		// 2. 进入流式输出阶段 (原生打印)
		// 打印 AI 的前缀
		fmt.Print("\033[1;35mAgent ❯\033[0m ")

		// 模拟调用 LLM API 并流式渲染
		responseContent := streamLLMResponse(input)

		// 记录 AI 状态
		history = append(history, Message{Role: "assistant", Content: responseContent})

		// 打印一个空行，为下一轮对话留出呼吸感
		fmt.Println("\n")
	}
}

// streamLLMResponse 模拟流式渲染过程
func streamLLMResponse(query string) string {
	// 实际开发中，这里应该是遍历你调用的 API (如 Claude/Gemini) 的 Stream Channel
	// 这里用一个固定的字符串和 time.Sleep 模拟打字机效果
	
	mockResponse := fmt.Sprintf("收到指令。正在处理针对 '%s' 的任务...\n", query)
	mockResponse += "✓ 分析上下文\n"
	mockResponse += "✓ 规划 Multi-Agent 协作路径\n"
	mockResponse += "准备就绪。"

	// 将字符串拆分成字符，模拟流式 Chunk 接收
	chars := strings.Split(mockResponse, "")
	var fullContent strings.Builder

	for _, char := range chars {
		// 原生流式输出的核心：使用 Print 而不是 Println
		fmt.Print(char)
		fullContent.WriteString(char)
		
		// 模拟网络延迟和推断速度
		time.Sleep(30 * time.Millisecond)
	}
	
	// 最后补一个换行
	fmt.Println()

	return fullContent.String()
}