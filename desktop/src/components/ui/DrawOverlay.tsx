import React, { useRef, useState } from 'react';
import { cn } from '@/lib/utils';
import { PreviewCommentSnapshot } from '@/types/annotation';

export interface Point {
  x: number;
  y: number;
}

export interface ColoredStroke {
  points: Point[];
  color: string;
}

interface DrawOverlayProps {
  mode: 'click' | 'draw' | 'interact';
  hoveredTarget: PreviewCommentSnapshot | null;
  selectedTarget: PreviewCommentSnapshot | null;
  strokes: ColoredStroke[];
  onStrokesChange: (strokes: ColoredStroke[]) => void;
  currentColor: string;
  className?: string;
  scrollOffset?: { x: number; y: number };
}

export function DrawOverlay({
  mode,
  hoveredTarget,
  selectedTarget,
  strokes,
  onStrokesChange,
  currentColor,
  className,
  scrollOffset = { x: 0, y: 0 },
}: DrawOverlayProps) {
  const [currentStroke, setCurrentStroke] = useState<ColoredStroke | null>(null);
  const containerRef = useRef<HTMLDivElement>(null);

  // We only handle drawing here. Element picking is handled by the iframe bridge
  // sending messages to the parent, which passes hoveredTarget/selectedTarget down.

  const handlePointerDown = (e: React.PointerEvent) => {
    if (mode !== 'draw' || e.button !== 0) return;
    const rect = containerRef.current?.getBoundingClientRect();
    if (!rect) return;
    
    e.preventDefault();
    setCurrentStroke({
      points: [{ x: e.clientX - rect.left + scrollOffset.x, y: e.clientY - rect.top + scrollOffset.y }],
      color: currentColor,
    });
  };

  const handlePointerMove = (e: React.PointerEvent) => {
    if (mode !== 'draw' || !currentStroke) return;
    const rect = containerRef.current?.getBoundingClientRect();
    if (!rect) return;
    
    e.preventDefault();
    setCurrentStroke({
      ...currentStroke,
      points: [...currentStroke.points, { x: e.clientX - rect.left + scrollOffset.x, y: e.clientY - rect.top + scrollOffset.y }],
    });
  };

  const handlePointerUp = (e: React.PointerEvent) => {
    if (mode !== 'draw' || !currentStroke) return;
    e.preventDefault();
    onStrokesChange([...strokes, currentStroke]);
    setCurrentStroke(null);
  };

  const renderPath = (stroke: ColoredStroke) => {
    if (stroke.points.length < 2) return null;
    const d = stroke.points.map((p, i) => (i === 0 ? `M ${p.x} ${p.y}` : `L ${p.x} ${p.y}`)).join(' ');
    return <path d={d} stroke={stroke.color} strokeWidth="3" fill="none" strokeLinecap="round" strokeLinejoin="round" />;
  };

  return (
    <div
      ref={containerRef}
      className={cn(
        'absolute inset-0 z-50 overflow-hidden',
        mode === 'draw' ? 'pointer-events-auto cursor-crosshair' : 'pointer-events-none',
        mode === 'interact' ? 'pointer-events-none' : '',
        className
      )}
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={handlePointerUp}
      onPointerLeave={handlePointerUp}
      onPointerCancel={handlePointerUp}
      style={{ touchAction: 'none' }}
    >
      <svg className="absolute inset-0 w-full h-full pointer-events-none">
        <g style={{ transform: `translate(${-scrollOffset.x}px, ${-scrollOffset.y}px)` }}>
          {strokes.map((s, i) => <g key={i}>{renderPath(s)}</g>)}
          {currentStroke && renderPath(currentStroke)}
        </g>
      </svg>

      {mode === 'click' && hoveredTarget && (
        <div
          className="absolute border-2 border-dashed border-primary bg-primary/10 pointer-events-none transition-all duration-75 flex items-end justify-end p-1"
          style={{
            left: hoveredTarget.position.x - 4,
            top: hoveredTarget.position.y - 4,
            width: hoveredTarget.position.width + 8,
            height: hoveredTarget.position.height + 8,
          }}
        >
          <span className="bg-primary text-primary-foreground text-[10px] px-1.5 py-0.5 rounded shadow-sm">
            {hoveredTarget.label}
          </span>
        </div>
      )}

      {selectedTarget && (
        <div
          className="absolute border-2 border-primary bg-primary/5 pointer-events-none transition-all duration-75"
          style={{
            left: selectedTarget.position.x - 4,
            top: selectedTarget.position.y - 4,
            width: selectedTarget.position.width + 8,
            height: selectedTarget.position.height + 8,
          }}
        >
           <div className="absolute -top-6 left-[-2px] bg-primary text-primary-foreground text-xs px-2 py-0.5 rounded-t">
              Selected: {selectedTarget.label}
           </div>
        </div>
      )}
    </div>
  );
}
