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

export function groupSegments(segments: ChatMessage['segments']): GroupedItem[] {
  const grouped: GroupedItem[] = []
  let currentGroup: GroupedWorkedSegment[] = []
  let currentGroupStart: number | undefined
  let currentDelegationGroup: GroupedWorkedSegment[] = []

  const flushWorked = () => {
    if (currentGroup.length > 0) {
      const hasToolCalls = currentGroup.some((s) => s.segment.type === 'tool_call')
      const firstOriginalIndex = currentGroupStart ?? currentGroup[0].originalIndex
      grouped.push({
        type: 'worked',
        id: `worked-${firstOriginalIndex}`,
        segments: [...currentGroup],
        hasToolCalls,
        isLast: false,
      })
      currentGroup = []
    }
    currentGroupStart = undefined
  }

  const flushDelegation = () => {
    if (currentDelegationGroup.length > 0) {
      const firstOriginalIndex = currentDelegationGroup[0].originalIndex
      grouped.push({
        type: 'delegation_group',
        id: `delegation-${firstOriginalIndex}-${currentDelegationGroup.length}`,
        segments: [...currentDelegationGroup],
      })
      currentDelegationGroup = []
    }
  }

  for (let i = 0; i < segments.length; i++) {
    const seg = segments[i]

    if (seg.type === 'thinking' && !seg.text.trim()) {
      // Keep the identity of a streaming placeholder as work is appended,
      // without rendering empty thinking before content or delegation.
      currentGroupStart ??= currentGroup[0]?.originalIndex ?? i
      if (i !== segments.length - 1) {
        continue
      }
    }


    const isDelegation =
      seg.type === 'delegation' ||
      (seg.type === 'tool_call' && (seg.name === 'delegate' || seg.name.startsWith('delegate_') || seg.name === 'request_team_help'))

    if (isDelegation) {
      flushWorked()
      currentDelegationGroup.push({ segment: seg, originalIndex: i })
    } else if (seg.type === 'thinking' || seg.type === 'tool_call' || seg.type === 'compact') {
      flushDelegation()
      currentGroup.push({ segment: seg, originalIndex: i })
    } else {
      flushWorked()
      flushDelegation()
      grouped.push({
        type: 'other',
        segment: seg,
        index: i,
      })
    }
  }
  flushWorked()
  flushDelegation()

  for (let i = grouped.length - 1; i >= 0; i--) {
    if (grouped[i].type === 'worked') {
      ;(grouped[i] as GroupedWorked).isLast = i === grouped.length - 1
      break
    }
  }

  return grouped
}
