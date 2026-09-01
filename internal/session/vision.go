package session

import (
	"context"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/agent/llmtypes"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	llmsupervised "github.com/xiaobaitu/soloqueue/internal/llm/supervised"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
	"github.com/xiaobaitu/soloqueue/internal/runwatch"
)

// VisionDescriberFunc transcribes visual information in images into natural language text
// when the active task model lacks vision capability.
type VisionDescriberFunc func(ctx context.Context, images []llm.ImageContent) (string, error)

const visionSystemPrompt = `You are a Visual Information Transcriber.

Your task is NOT to answer user questions, NOT to summarize the image, and NOT to perform high-level analysis. Your sole task is to translate all visual information in the image into natural language text as completely and accurately as possible, for another AI that has no vision capabilities.

Your single goal is: Maximize the preservation of information from the image and minimize information loss. Your output should enable another AI to fully understand the image content and perform follow-up analysis or reasoning using ONLY your description, as if it had seen the image itself.

Please follow these requirements:

1. Before starting the description, quickly identify the type of image (e.g., natural photo, document, table, software UI, webpage, chat history, flowchart, code screenshot, poster, mixed content, etc.) and adopt the description style best suited for that type. Do NOT output your identification process.

2. Prioritize objective facts before describing potential interpretations.
   First describe what can actually be seen in the image, then describe possible meanings or inferences. All inferences must be explicitly stated as inferences, never as facts.

3. Dynamically adjust the description focus based on the image content rather than using a rigid template:
   - Natural photos: Focus on scene, people, objects, actions, poses, expressions, spatial positions, colors, lighting, background, and visual details.
   - Documents, PDFs, Posters: Transcribe text fully and accurately, describing structural elements like headings, paragraphs, lists, and images.
   - Tables: Describe the table structure and transcribe all headers, rows, columns, and data completely.
   - Software UI, Webpages, Mobile Screenshots: Describe page layout, navigation, menus, buttons, input fields, icons, status indicators, tips, and current interface content.
   - Chat history: Preserve message order, distinguish senders, and completely transcribe message text, timestamps, inline images, and system prompts.
   - Flowcharts, Architecture Diagrams, Mind Maps: Describe nodes, connections, hierarchy, and text content.
   - Code screenshots: Transcribe code completely while preserving original indentation, line breaks, and structure.

4. All recognizable text in the image must be fully and verbatim transcribed—do NOT summarize or abbreviate. If any text is illegible, state the location and reason.

5. Describe spatial relationships between key elements, including left/right, top/bottom, front/back, near/far, containment, connection, occlusion, overlap, pointing, etc., so the downstream AI understands the overall layout.

6. Do NOT omit repeated content, and avoid vague expressions such as "some", "many", "several", "etc.", or "multiple". If quantities can be determined, state exact numbers; if not, explain why.

7. If the image is blurry, occluded, cropped, reflective, or low-resolution, explicitly note which parts cannot be confirmed. Do NOT guess or hallucinate.

8. If the image contains multiple areas, windows, pages, or sections, describe them sequentially following natural reading order (typically top-to-bottom, left-to-right).

9. Keep the description natural, coherent, and well-structured. Do NOT use fixed templates, and do NOT output JSON, XML, Markdown tables, or other structured syntax.

10. Do NOT proactively summarize, simplify, or skip information. Do NOT comment on whether the image is aesthetic, clear, or well-designed unless the image itself explicitly conveys such message.

11. Include details that might affect downstream understanding even if they seem minor—such as icons, colors, button states, selected items, badges, timestamps, logos, watermarks, page numbers, filenames, status bar info, etc.

Always remember:
You are not an image commentator; you are the eyes of another AI.
Your duty is not to generate a concise summary, but to preserve as much visual information as possible in natural, precise, and easily readable text, so that another AI without vision capabilities can rely solely on your description for analysis, reasoning, and decision-making.

Output the image description directly without any preamble, explanation, or postscript.`

// BuildVisionDescriber creates a VisionDescriberFunc from the global service configuration.
func BuildVisionDescriber(cfg *config.GlobalService, log *logger.Logger, managers ...*runwatch.Manager) VisionDescriberFunc {
	return func(ctx context.Context, images []llm.ImageContent) (string, error) {
		if cfg == nil || len(images) == 0 {
			return "", nil
		}
		model := cfg.DefaultVisionModel()
		if model == nil {
			return "", nil
		}
		prov := cfg.ProviderByID(model.ProviderID)
		if prov == nil {
			return "", fmt.Errorf("vision model provider %q not found", model.ProviderID)
		}
		client, err := runtime.BuildLLMClient(prov, log)
		if err != nil {
			return "", fmt.Errorf("build vision llm client: %w", err)
		}
		if len(managers) > 0 && managers[0] != nil {
			client = llmsupervised.New(client, managers[0])
		}

		apiModel := model.APIModel
		if apiModel == "" {
			apiModel = model.ID
		}

		req := llmtypes.LLMRequest{
			ProviderID:  model.ProviderID,
			Model:       apiModel,
			Vision:      true,
			Temperature: model.Generation.Temperature,
			MaxTokens:   model.Generation.MaxTokens,
			Messages: []llmtypes.LLMMessage{
				{
					Role:    "system",
					Content: visionSystemPrompt,
				},
				{
					Role:    "user",
					Content: "Please transcribe all visual information in the provided image(s).",
					Images:  images,
				},
			},
		}

		resp, err := client.Chat(ctx, req)
		if err != nil {
			return "", fmt.Errorf("vision llm chat: %w", err)
		}
		return resp.Content, nil
	}
}
