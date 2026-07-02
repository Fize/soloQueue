package simulation

import (
	"fmt"
	"strings"
)

// ActionType enumerates the possible actions an agent can take.
type ActionType string

const (
	ActionSpeak    ActionType = "speak"
	ActionMove     ActionType = "move"
	ActionInteract ActionType = "interact"
	ActionWait     ActionType = "wait"
	ActionPass     ActionType = "pass"
	ActionSpawn    ActionType = "spawn"
	ActionDie      ActionType = "die"
	ActionConflict ActionType = "conflict"
	ActionHide     ActionType = "hide"
)

// proposal represents a [PROPOSE key: value] directive for WorldState updates.
type proposal struct {
	key   string
	value string
}

// Action represents a single action decided by an agent.
type Action struct {
	Type     ActionType `json:"type"`
	Target   string     `json:"target"`    // agent ID, zone name, object ID, or "*" for broadcast
	Content  string     `json:"content"`   // for speak: the message text; for interact: the action
	Duration string     `json:"duration,omitempty"` // for wait: how long (e.g. "30m")
}

// String returns the action formatted as a directive.
func (a Action) String() string {
	switch a.Type {
	case ActionSpeak:
		if a.Target == "*" {
			return fmt.Sprintf("[SAY]: %s", a.Content)
		}
		return fmt.Sprintf("[SAY @%s]: %s", a.Target, a.Content)
	case ActionMove:
		return fmt.Sprintf("[MOVE %s]", a.Target)
	case ActionInteract:
		return fmt.Sprintf("[INTERACT %s: %s]", a.Target, a.Content)
	case ActionWait:
		return fmt.Sprintf("[WAIT %s]", a.Duration)
	case ActionPass:
		return "[PASS]"
	case ActionSpawn:
		return fmt.Sprintf("[SPAWN %s]: %s", a.Target, a.Content)
	case ActionDie:
		return "[DIE]"
	case ActionConflict:
		return fmt.Sprintf("[CONFLICT @%s]: %s", a.Target, a.Content)
	case ActionHide:
		return "[HIDE]"
	default:
		return fmt.Sprintf("[UNKNOWN %s]", a.Type)
	}
}

// ParseActions extracts actions from an agent's LLM response.
// Recognizes the action directive syntax:
//   [SAY]: text                    → broadcast message
//   [SAY @name]: text              → directed message
//   [MOVE zone_name]               → move to zone
//   [INTERACT object: action]      → interact with object
//   [WAIT duration]                → wait
//   [PASS]                         → do nothing this tick
// Also recognizes legacy [PROPOSE key: value] for WorldState updates.
func ParseActions(content string) (actions []Action, proposals []proposal) {
	lines := strings.Split(content, "\n")
	inSay := false
	var sayTarget, sayContent strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Handle multi-line SAY blocks (content between [SAY...] and the end of paragraph)
		if inSay {
			if trimmed == "" || isActionLine(trimmed) {
				// End of SAY block
				actions = append(actions, Action{
					Type:    ActionSpeak,
					Target:  strings.TrimSpace(sayTarget.String()),
					Content: strings.TrimSpace(sayContent.String()),
				})
				sayTarget.Reset()
				sayContent.Reset()
				inSay = false
			} else {
				sayContent.WriteString(trimmed)
				sayContent.WriteString("\n")
				continue
			}
		}

		if trimmed == "" {
			continue
		}

		// [SAY]: ... (broadcast)
		if strings.HasPrefix(trimmed, "[SAY]:") {
			remainder := strings.TrimPrefix(trimmed, "[SAY]:")
			remainder = strings.TrimSpace(remainder)
			if remainder != "" {
				actions = append(actions, Action{
					Type:    ActionSpeak,
					Target:  "*",
					Content: remainder,
				})
			}
			continue
		}

		// [SAY @name]: ... (directed)
		if strings.HasPrefix(trimmed, "[SAY @") {
			atIdx := strings.Index(trimmed, "@")
			colonIdx := strings.Index(trimmed[atIdx:], "]:")
			if colonIdx != -1 {
				target := strings.TrimSpace(trimmed[atIdx+1 : atIdx+colonIdx])
				content := strings.TrimSpace(trimmed[atIdx+colonIdx+2:])
				if content != "" {
					actions = append(actions, Action{
						Type:    ActionSpeak,
						Target:  target,
						Content: content,
					})
				}
			}
			continue
		}

		// [MOVE zone_name]
		if strings.HasPrefix(trimmed, "[MOVE ") && strings.HasSuffix(trimmed, "]") {
			zone := strings.TrimPrefix(trimmed, "[MOVE ")
			zone = strings.TrimSuffix(zone, "]")
			zone = strings.TrimSpace(zone)
			if zone != "" {
				actions = append(actions, Action{
					Type:   ActionMove,
					Target: zone,
				})
			}
			continue
		}

		// [INTERACT object: action]
		if strings.HasPrefix(trimmed, "[INTERACT ") && strings.HasSuffix(trimmed, "]") {
			inner := strings.TrimPrefix(trimmed, "[INTERACT ")
			inner = strings.TrimSuffix(inner, "]")
			parts := strings.SplitN(inner, ":", 2)
			if len(parts) == 2 {
				actions = append(actions, Action{
					Type:    ActionInteract,
					Target:  strings.TrimSpace(parts[0]),
					Content: strings.TrimSpace(parts[1]),
				})
			}
			continue
		}

		// [WAIT duration]
		if strings.HasPrefix(trimmed, "[WAIT ") && strings.HasSuffix(trimmed, "]") {
			dur := strings.TrimPrefix(trimmed, "[WAIT ")
			dur = strings.TrimSuffix(dur, "]")
			dur = strings.TrimSpace(dur)
			if dur != "" {
				actions = append(actions, Action{
					Type:     ActionWait,
					Duration: dur,
				})
			}
			continue
		}

		// [PASS]
		if trimmed == "[PASS]" {
			actions = append(actions, Action{Type: ActionPass})
			continue
		}

		// [DIE] or [EXIT] — agent voluntarily leaves the simulation
		if trimmed == "[DIE]" || trimmed == "[EXIT]" {
			actions = append(actions, Action{Type: ActionDie})
			continue
		}

		// [SPAWN name]: description — introduce a new agent
		if strings.HasPrefix(trimmed, "[SPAWN ") {
			rest := strings.TrimPrefix(trimmed, "[SPAWN ")
			if idx := strings.Index(rest, "]: "); idx != -1 {
				name := strings.TrimSpace(rest[:idx])
				desc := strings.TrimSpace(rest[idx+3:])
				actions = append(actions, Action{
					Type:    ActionSpawn,
					Target:  name,
					Content: desc,
				})
			} else if strings.HasSuffix(rest, "]") {
				// [SPAWN name] without description
				name := strings.TrimSuffix(rest, "]")
				name = strings.TrimSpace(name)
				actions = append(actions, Action{
					Type:   ActionSpawn,
					Target: name,
				})
			}
			continue
		}

		// [CONFLICT @name]: ... (conflict/provocation)
		if strings.HasPrefix(trimmed, "[CONFLICT @") {
			atIdx := strings.Index(trimmed, "@")
			colonIdx := strings.Index(trimmed[atIdx:], "]:")
			if colonIdx != -1 {
				target := strings.TrimSpace(trimmed[atIdx+1 : atIdx+colonIdx])
				content := strings.TrimSpace(trimmed[atIdx+colonIdx+2:])
				if content != "" {
					actions = append(actions, Action{
						Type:    ActionConflict,
						Target:  target,
						Content: content,
					})
				}
			}
			continue
		}

		// [HIDE] (stealth/hide)
		if trimmed == "[HIDE]" {
			actions = append(actions, Action{Type: ActionHide})
			continue
		}

		// Legacy [PROPOSE key: value]
		if strings.HasPrefix(trimmed, "[PROPOSE ") && strings.HasSuffix(trimmed, "]") {
			inner := strings.TrimPrefix(trimmed, "[PROPOSE ")
			inner = strings.TrimSuffix(inner, "]")
			parts := strings.SplitN(inner, ":", 2)
			if len(parts) == 2 {
				proposals = append(proposals, proposal{
					key:   strings.TrimSpace(parts[0]),
					value: strings.TrimSpace(parts[1]),
				})
			}
			continue
		}
	}

	// Handle SAY block that ends at EOF
	if inSay {
		actions = append(actions, Action{
			Type:    ActionSpeak,
			Target:  strings.TrimSpace(sayTarget.String()),
			Content: strings.TrimSpace(sayContent.String()),
		})
	}

	return actions, proposals
}

func isActionLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "[SAY]") || strings.HasPrefix(trimmed, "[SAY @") ||
		strings.HasPrefix(trimmed, "[MOVE ") || strings.HasPrefix(trimmed, "[INTERACT ") ||
		strings.HasPrefix(trimmed, "[WAIT ") || trimmed == "[PASS]" ||
		strings.HasPrefix(trimmed, "[PROPOSE ") || strings.HasPrefix(trimmed, "[CONFLICT ") ||
		trimmed == "[HIDE]" ||
		strings.HasPrefix(trimmed, "[SPAWN ") || trimmed == "[DIE]" || trimmed == "[EXIT]"
}

// FormatActionsForPrompt generates the action syntax documentation for system prompts.
func FormatActionsForPrompt() string {
	return `## Available Actions
You may take ONE of the following actions per response:

1. Speak (broadcast to everyone in your zone):
   [SAY]: Your message here.

2. Speak to a specific person (private):
   [SAY @agent_name]: Your private message here.

3. Move to another zone:
   [MOVE zone_name]

4. Interact with an object:
   [INTERACT object_name]: action description

5. Wait for some time:
   [WAIT 30m]

6. Do nothing this turn:
   [PASS]

7. Initiate conflict or fight back against someone (physical or verbal conflict):
   [CONFLICT @agent_name]: details of the conflict (e.g. provoke, attack, grab item)
   *Constraints*: Assess your faction's total strength (including present allies) vs their faction's total strength first. Hide attacks grant surprise combat bonuses.

8. Hide yourself in your current zone (stealth):
   [HIDE]
   *Constraints*: Puts you in hidden state. You become invisible to others unless their perception beats your stealth. Moving automatically unhides you. Sneak attacks from hiding grant +30 combat strength.

You may also propose changes to the shared world state:
   [PROPOSE key]: value

Special life-changing actions (use sparingly):
9. Spawn a new character into the world:
   [SPAWN character_name]: brief description of who they are and their relationship to you.
   *Constraints*: The spawned agent must be someone you already know (e.g., mentor, disciple, family, rival in your bio). You are ONLY allowed to have a fortuitous encounter with a stranger if the World State has "adventure": true or "fortuitous encounter": true.

10. Leave the simulation permanently (your role is complete or you were killed):
    [DIE]

Important: [SPAWN] and [DIE] are permanent. Use [DIE] only when your character's story is truly complete. Use [SPAWN] only when a genuinely new perspective is needed.

You can also update how you feel about another person:
   [RELATION name: kind=friend, affinity=+0.2, tags=reliable,trustworthy]
   kind options: friend, rival, colleague, mentor, mentee, neighbor, sibling, stranger
   affinity: -1.0 to 1.0 (use + or - prefix for relative change)`
}

// FormatActionsForPromptInLanguage generates the action syntax documentation in the target language.
func FormatActionsForPromptInLanguage(lang string) string {
	if lang == "zh" {
		return `## Available Actions
You may take ONE of the following actions per response:

1. Speak (broadcast to everyone in your zone):
   [SAY]: Your message here.

2. Speak to a specific person (private):
   [SAY @agent_name]: Your private message here.

3. Move to another zone:
   [MOVE zone_name]

4. Interact with an object:
   [INTERACT object_name]: action description

5. Wait for some time:
   [WAIT 30m]

6. Do nothing this turn:
   [PASS]

7. Initiate conflict or fight back against someone (physical or verbal conflict):
   [CONFLICT @agent_name]: details of the conflict (e.g. provoke, attack, grab item)
   *Constraints*: First assess the total combat power of your faction (including present allies) compared to the enemy faction's total combat power. Stealth attacks grant surprise combat bonuses.

8. Hide yourself in your current zone (stealth):
   [HIDE]
   *Constraints*: Puts you in hidden state. You become invisible to others unless their perception exceeds your stealth. Moving automatically unhides you. Sneak attacks from hiding grant +30 combat strength.

You may also propose changes to the shared world state:
   [PROPOSE key]: value

Special life-changing actions (use sparingly):
9. Spawn a new character into the world:
   [SPAWN character_name]: brief description of who they are and their relationship to you.
   *Constraints*: The spawned agent must be someone you already know (e.g., mentor, disciple, family, rival in your life experience). You are ONLY allowed to have a fortuitous encounter with a stranger if the World State contains "adventure": true or "fortuitous encounter": true.

10. Leave the simulation permanently (your character is complete or has been killed):
    [DIE]

Important: [SPAWN] and [DIE] are irreversible. Use [DIE] only when your character's story is truly complete. Use [SPAWN] only when a genuinely new perspective is needed.

You can also update your feelings towards another person:
   [RELATION name: kind=friend, affinity=+0.2, tags=reliable,trustworthy]
   kind options: friend, rival, colleague, mentor, mentee, neighbor, sibling, stranger
   affinity: -1.0 to 1.0 (use + or - prefix for relative change)`
	}
	return FormatActionsForPrompt()
}