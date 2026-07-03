export interface PreviewCommentSnapshot {
  filePath: string;
  elementId: string;
  selector: string;
  label: string;
  text: string;
  position: { x: number; y: number; width: number; height: number };
  hoverPoint?: { x: number; y: number };
  htmlHint: string;
  style?: Record<string, string>;
  selectionKind?: 'element' | 'pod' | 'visual';
  memberCount?: number;
  podMembers?: PreviewCommentMember[];
}

export interface PreviewCommentMember {
  elementId: string;
  selector: string;
  label: string;
  text: string;
  position: { x: number; y: number; width: number; height: number };
  htmlHint: string;
  style?: Record<string, string>;
}

export type PreviewVisualMarkKind = 'click' | 'stroke' | 'click+stroke';

export interface ChatCommentAttachment {
  id: string;
  order: number;
  filePath: string;
  elementId: string;
  selector: string;
  label: string;
  comment: string;
  currentText: string;
  pagePosition: { x: number; y: number; width: number; height: number };
  htmlHint: string;
  style?: Record<string, string>;
  selectionKind: 'element' | 'pod' | 'visual';
  memberCount?: number;
  podMembers?: PreviewCommentMember[];
  screenshotPath?: string;
  screenshotBase64?: string;
  markKind?: PreviewVisualMarkKind;
  intent?: string;
  source: 'board-batch' | 'saved-comment';
}
