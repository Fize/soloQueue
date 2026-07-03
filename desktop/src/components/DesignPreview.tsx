import { useState, useEffect, useRef, useMemo } from 'react';
import { injectSelectionBridge } from '@/utils/iframeBridge';
import { DrawOverlay } from './ui/DrawOverlay';
import { ArrowLeft, ArrowRight, RotateCcw, Home } from 'lucide-react';
import type { PreviewCommentSnapshot } from '@/types/annotation';
import type { ColoredStroke } from './ui/DrawOverlay';

interface DesignPreviewProps {
  htmlContent: string;
  mode: 'click' | 'draw' | 'interact';
  onHoverTarget?: (target: PreviewCommentSnapshot | null) => void;
  onSelectTarget?: (target: PreviewCommentSnapshot | null) => void;
  onStrokesChange?: (strokes: ColoredStroke[]) => void;
  strokes?: ColoredStroke[];
  currentColor?: string;
}

export function DesignPreview({
  htmlContent,
  mode,
  onHoverTarget,
  onSelectTarget,
  onStrokesChange,
  strokes = [],
  currentColor = 'hsl(var(--destructive))',
}: DesignPreviewProps) {
  const [hoveredTarget, setHoveredTarget] = useState<PreviewCommentSnapshot | null>(null);
  const [selectedTarget, setSelectedTarget] = useState<PreviewCommentSnapshot | null>(null);
  const [scrollOffset, setScrollOffset] = useState({ x: 0, y: 0 });
  const iframeRef = useRef<HTMLIFrameElement>(null);

  // Inject the bridge script into the provided HTML content
  const srcDoc = useMemo(() => {
    return injectSelectionBridge(htmlContent);
  }, [htmlContent]);

  // Reset scrollOffset when srcDoc changes (iframe reloads)
  useEffect(() => {
    setScrollOffset({ x: 0, y: 0 });
  }, [srcDoc]);

  const getIframeMode = () => {
    if (mode === 'draw') return 'pod';
    if (mode === 'click') return 'picker';
    return 'interact';
  };

  useEffect(() => {
    // Tell the iframe which mode we are in so it can enable/disable its listeners
    const iframeWindow = iframeRef.current?.contentWindow;
    if (iframeWindow) {
      try {
        iframeWindow.postMessage(
          { type: 'od:comment-mode', enabled: true, mode: getIframeMode() },
          '*'
        );
      } catch (e) {
        console.error('Failed to post mode to iframe', e);
      }
    }
  }, [mode, srcDoc]); // re-run if srcDoc changes (iframe reloads)

  useEffect(() => {
    const handleMessage = (ev: MessageEvent) => {
      const data = ev.data;
      if (!data || typeof data.type !== 'string') return;

      if (data.type === 'od:comment-hover') {
        setHoveredTarget(data as PreviewCommentSnapshot);
        onHoverTarget?.(data as PreviewCommentSnapshot);
      } else if (data.type === 'od:comment-leave') {
        setHoveredTarget(null);
        onHoverTarget?.(null);
      } else if (data.type === 'od:comment-target') {
        // User clicked a target
        setSelectedTarget(data as PreviewCommentSnapshot);
        onSelectTarget?.(data as PreviewCommentSnapshot);
      } else if (data.type === 'od:iframe-scroll') {
        setScrollOffset({ x: data.x, y: data.y });
      } else if (data.type === 'od:comment-scroll-selected') {
        setSelectedTarget(data as PreviewCommentSnapshot);
      } else if (data.type === 'od:comment-scroll-hovered') {
        setHoveredTarget(data as PreviewCommentSnapshot);
      }
    };

    window.addEventListener('message', handleMessage);
    return () => window.removeEventListener('message', handleMessage);
  }, [onHoverTarget, onSelectTarget]);

  const navigate = (action: 'back' | 'forward' | 'reload') => {
    try {
      const win = iframeRef.current?.contentWindow;
      if (!win) return;
      if (action === 'back') win.history.back();
      else if (action === 'forward') win.history.forward();
      else if (action === 'reload') win.location.reload();
    } catch {
      // sandboxed iframe may block navigation
    }
  };

  return (
    <div className="relative w-full h-full flex flex-col bg-background">
      {/* Iframe navigation bar */}
      <div className="h-8 flex items-center gap-1 px-2 border-b border-border/30 bg-muted/20 shrink-0">
        <button
          onClick={() => navigate('back')}
          className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
          title="Back"
        >
          <ArrowLeft className="h-3.5 w-3.5" />
        </button>
        <button
          onClick={() => navigate('forward')}
          className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
          title="Forward"
        >
          <ArrowRight className="h-3.5 w-3.5" />
        </button>
        <button
          onClick={() => navigate('reload')}
          className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
          title="Reload"
        >
          <RotateCcw className="h-3.5 w-3.5" />
        </button>
        <div className="w-px h-4 bg-border/40 mx-1" />
        <button
          onClick={() => {
            // Reset iframe to original srcDoc by re-triggering load
            const iframe = iframeRef.current;
            if (iframe) {
              const current = iframe.srcdoc;
              iframe.srcdoc = '';
              requestAnimationFrame(() => { iframe.srcdoc = current; });
            }
          }}
          className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
          title="Home (Reset to original)"
        >
          <Home className="h-3.5 w-3.5" />
        </button>
        <span className="text-[10px] text-muted-foreground/60 ml-auto truncate max-w-[200px] font-mono select-none">
          Preview
        </span>
      </div>
      <iframe
        ref={iframeRef}
        srcDoc={srcDoc}
        className="flex-1 w-full border-none"
        sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
        onLoad={() => {
          // Re-sync mode when iframe finishes loading
          const iframeWindow = iframeRef.current?.contentWindow;
          if (iframeWindow) {
            iframeWindow.postMessage(
              { type: 'od:comment-mode', enabled: true, mode: getIframeMode() },
              '*'
            );
          }
        }}
      />
      <DrawOverlay
        mode={mode}
        hoveredTarget={hoveredTarget}
        selectedTarget={selectedTarget}
        strokes={strokes}
        currentColor={currentColor}
        onStrokesChange={(s) => {
          onStrokesChange?.(s);
        }}
        scrollOffset={scrollOffset}
      />
    </div>
  );
}
