import { ChatCommentAttachment, PreviewVisualMarkKind } from '../types/annotation';

export function messageContentWithCommentAttachments(
  content: string,
  commentAttachments: ChatCommentAttachment[],
): string {
  if (commentAttachments.length === 0) return content;
  const visibleContent = content.trim() || '(No extra typed instruction.)';
  return `${visibleContent}\n${renderCommentAttachmentContext(commentAttachments)}`;
}

export function trimContextText(value: string): string {
  const text = String(value || '').replace(/\s+/g, ' ').trim();
  return text.length > 160 ? `${text.slice(0, 157)}...` : text;
}

export function trimHtmlHint(value: string): string {
  const text = String(value || '').replace(/\s+/g, ' ').trim();
  return text.length > 180 ? `${text.slice(0, 177)}...` : text;
}

function renderCommentAttachmentContext(commentAttachments: ChatCommentAttachment[]): string {
  const lines = [
    '',
    '<attached-preview-comments>',
    'Scope: apply the user request to the attached preview target by default. For visual marks, inspect the screenshot and modify the marked region first. Preserve unrelated elements.',
  ];
  commentAttachments.forEach((item) => {
    const selectionKind = item.selectionKind === 'visual' ? 'visual' : item.selectionKind === 'pod' ? 'pod' : 'element';
    lines.push(
      '',
      `${item.order}. ${item.elementId}`,
      `targetKind: ${selectionKind}`,
      `file: ${item.filePath}`,
      `label: ${item.label || '(unlabeled)'}`,
      `position: x${item.pagePosition.x} y${item.pagePosition.y} ${item.pagePosition.width}x${item.pagePosition.height}`,
      `currentText: ${trimContextText(item.currentText || '') || '(empty)'}`,
      `htmlHint: ${trimHtmlHint(item.htmlHint || '') || '(none)'}`,
      `computedStyle: ${formatAnnotationStyle(item.style) || '(none)'}`,
      `comment: ${item.comment}`,
    );
    if (selectionKind === 'visual') {
      lines.push(
        `markKind: ${item.markKind || 'stroke'}`,
        `intent: ${item.intent || visualAnnotationIntent(item.markKind || 'stroke')}`,
      );
      if (item.selector) lines.push(`selector: ${item.selector}`);
    } else {
      lines.splice(lines.length - 4, 0, `selector: ${item.selector}`);
    }
    if (selectionKind === 'pod') {
      lines.push(`memberCount: ${item.memberCount || item.podMembers?.length || 0}`);
      (item.podMembers ?? []).slice(0, 8).forEach((member, memberIndex) => {
        lines.push(
          `member.${memberIndex + 1}: ${member.elementId} | ${member.label || '(unlabeled)'} | ${member.selector}`,
        );
        const memberStyle = formatAnnotationStyle(member.style);
        if (memberStyle) lines.push(`member.${memberIndex + 1}.computedStyle: ${memberStyle}`);
      });
    }
  });
  lines.push('</attached-preview-comments>');
  return lines.join('\n');
}

function visualAnnotationIntent(markKind: PreviewVisualMarkKind): string {
  if (markKind === 'click') {
    return 'The screenshot has a blue focus box around the picked element; modify that picked part first.';
  }
  if (markKind === 'click+stroke') {
    return 'The screenshot has a blue focus box and red strokes; together they identify the part the user wants changed.';
  }
  return 'The screenshot has red strokes that identify the visual region the user wants changed.';
}

function formatAnnotationStyle(style: Record<string, string> | undefined): string {
  if (!style) return '';
  return Object.entries(style)
    .filter(([_, value]) => Boolean(value))
    .map(([key, value]) => `${key}: ${value}`)
    .join('; ');
}
