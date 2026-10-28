export interface ChatDraft {
  text: string
  mentions: Map<string, string>
}

const drafts = new Map<string, ChatDraft>()

export function getChatDraft(key: string): ChatDraft {
  return drafts.get(key) ?? { text: '', mentions: new Map() }
}

export function setChatDraft(key: string, draft: ChatDraft): void {
  if (!draft.text && draft.mentions.size === 0) drafts.delete(key)
  else drafts.set(key, draft)
}

export function clearChatDraft(key: string): void {
  drafts.delete(key)
}
