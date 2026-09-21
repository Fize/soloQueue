import type { ChatMessage } from '@/types'

export interface GroupedWorkedSegment {
  segment: ChatMessage['segments'][number]
  originalIndex: number
}

export interface GroupedWorked {
  type: 'worked'
  id: string
  segments: GroupedWorkedSegment[]
  hasToolCalls: boolean
  isLast: boolean
}

export interface GroupedDelegation {
  type: 'delegation_group'
  id: string
  segments: GroupedWorkedSegment[]
}

interface GroupedOther {
  type: 'other'
  segment: ChatMessage['segments'][number]
  index: number
}

type GroupedItem = GroupedWorked | GroupedDelegation | GroupedOther

function isDelegation(segment: ChatMessage['segments'][number]): boolean {
  return segment.type === 'delegation' || (
    segment.type === 'tool_call' && (
      segment.name === 'delegate' ||
      segment.name.startsWith('delegate_') ||
      segment.name === 'request_team_help'
    )
  )
}

/**
 * Preserve the event stream's order inside one assistant message. Only
 * adjacent thinking/tool events share a worked container; the events inside
 * that container remain separate, and every boundary starts a new item.
 */
export function groupSegments(segments: ChatMessage['segments']): GroupedItem[] {
  const groups: GroupedItem[] = []
  let emptyThinkingAnchor: number | undefined

  for (let index = 0; index < segments.length; index++) {
    const segment = segments[index]

    if (segment.type === 'thinking' && !segment.text.trim()) {
      emptyThinkingAnchor ??= index
      continue
    }

    if (isDelegation(segment)) {
      groups.push({
        type: 'delegation_group',
        id: `delegation-${index}`,
        segments: [{ segment, originalIndex: index }],
      })
      continue
    }

    if (segment.type === 'thinking' || segment.type === 'tool_call') {
      const previous = groups.at(-1)
      if (previous?.type === 'worked') {
        previous.segments.push({ segment, originalIndex: index })
        previous.hasToolCalls ||= segment.type === 'tool_call'
      } else {
        const idIndex = emptyThinkingAnchor ?? index
        groups.push({
          type: 'worked',
          id: `worked-${idIndex}`,
          segments: [{ segment, originalIndex: index }],
          hasToolCalls: segment.type === 'tool_call',
          isLast: false,
        })
        emptyThinkingAnchor = undefined
      }
      continue
    }

    groups.push({ type: 'other', segment, index })
  }

  // The empty thinking emitted at stream start is a visible running anchor
  // only until real content arrives. If work later arrives, its ID consumes
  // the anchor above while its visual position remains chronological.
  if (groups.length === 0 && emptyThinkingAnchor !== undefined) {
    groups.push({
      type: 'worked',
      id: `worked-${emptyThinkingAnchor}`,
      segments: [{ segment: segments[emptyThinkingAnchor], originalIndex: emptyThinkingAnchor }],
      hasToolCalls: false,
      isLast: true,
    })
  }

  groups.forEach((group, index) => {
    if (group.type === 'worked') group.isLast = index === groups.length - 1
  })
  return groups
}
