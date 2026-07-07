import { useState, useEffect, useRef, useMemo } from 'react';
import { injectSelectionBridge } from '@/utils/iframeBridge';
import { DrawOverlay } from './ui/DrawOverlay';
import { ArrowLeft, ArrowRight, RotateCcw, Home, Monitor, Smartphone, Tablet, ChevronDown, Check, X } from 'lucide-react';
import type { PreviewCommentSnapshot } from '@/types/annotation';
import type { ColoredStroke } from './ui/DrawOverlay';
import { cn } from '@/lib/utils';

// ─── Device preview types ────────────────────────────────────────────────────

interface Device {
  name: string;
  width: number;
  height: number;
  category: 'responsive' | 'ios-phone' | 'android-phone' | 'ios-tablet' | 'android-tablet';
}

const DEVICES: Device[] = [
  { name: 'Responsive', width: 0, height: 0, category: 'responsive' },
  { name: 'iPhone SE', width: 375, height: 667, category: 'ios-phone' },
  { name: 'iPhone 14 Pro', width: 393, height: 852, category: 'ios-phone' },
  { name: 'iPhone 14 Pro Max', width: 430, height: 932, category: 'ios-phone' },
  { name: 'Pixel 7', width: 412, height: 915, category: 'android-phone' },
  { name: 'Galaxy S20', width: 360, height: 800, category: 'android-phone' },
  { name: 'iPad Mini', width: 768, height: 1024, category: 'ios-tablet' },
  { name: 'iPad Pro 11"', width: 834, height: 1194, category: 'ios-tablet' },
  { name: 'iPad Pro 12.9"', width: 1024, height: 1366, category: 'ios-tablet' },
  { name: 'Galaxy Tab S8', width: 800, height: 1280, category: 'android-tablet' },
];

const CATEGORY_LABELS: Record<Device['category'], string> = {
  'responsive': 'Responsive',
  'ios-phone': 'iOS Phone',
  'android-phone': 'Android Phone',
  'ios-tablet': 'iOS Tablet',
  'android-tablet': 'Android Tablet',
};

const CATEGORY_ICONS: Record<Device['category'], typeof Monitor> = {
  'responsive': Monitor,
  'ios-phone': Smartphone,
  'android-phone': Smartphone,
  'ios-tablet': Tablet,
  'android-tablet': Tablet,
};

// Group devices by category for dropdown display
const DEVICE_GROUPS = (() => {
  const groups = new Map<Device['category'], Device[]>();
  for (const d of DEVICES) {
    const list = groups.get(d.category) || [];
    list.push(d);
    groups.set(d.category, list);
  }
  // Keep responsive first, then phones, then tablets
  const order: Device['category'][] = ['responsive', 'ios-phone', 'android-phone', 'ios-tablet', 'android-tablet'];
  return order.map(cat => ({ category: cat, devices: groups.get(cat) || [] }));
})();

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

  // Device preview state
  const [selectedDevice, setSelectedDevice] = useState<Device | null>(null);
  const [showDeviceDropdown, setShowDeviceDropdown] = useState(false);
  const deviceDropdownRef = useRef<HTMLDivElement>(null);

  // Close device dropdown when clicking outside
  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (deviceDropdownRef.current && !deviceDropdownRef.current.contains(e.target as Node)) {
        setShowDeviceDropdown(false);
      }
    }
    document.addEventListener('click', handleClickOutside);
    return () => document.removeEventListener('click', handleClickOutside);
  }, []);

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
  }, [mode, srcDoc]);

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
      {/* ── Browser-style menu bar ─────────────────────────────────────────── */}
      <div className="h-9 flex items-center gap-1 px-2 border-b border-border/30 bg-muted/20 shrink-0 select-none">
        {/* Browser nav cluster */}
        <button
          onClick={() => navigate('back')}
          className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
          title="Back"
        >
          <ArrowLeft className="h-3.5 w-3.5" />
        </button>
        <button
          onClick={() => navigate('forward')}
          className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
          title="Forward"
        >
          <ArrowRight className="h-3.5 w-3.5" />
        </button>
        <div className="w-px h-4 bg-border/40 mx-0.5" />
        <button
          onClick={() => navigate('reload')}
          className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
          title="Reload"
        >
          <RotateCcw className="h-3.5 w-3.5" />
        </button>
        <button
          onClick={() => {
            const iframe = iframeRef.current;
            if (iframe) {
              const current = iframe.srcdoc;
              iframe.srcdoc = '';
              requestAnimationFrame(() => { iframe.srcdoc = current; });
            }
          }}
          className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
          title="Home (Reset to original)"
        >
          <Home className="h-3.5 w-3.5" />
        </button>

        {/* Divider + Device selector */}
        <div className="w-px h-4 bg-border/40 mx-1" />
        <div className="relative shrink-0" ref={deviceDropdownRef}>
          <button
            onClick={(e) => { e.stopPropagation(); setShowDeviceDropdown(!showDeviceDropdown); }}
            className={cn(
              "flex items-center gap-1 px-2 h-7 rounded text-xs font-medium transition-colors cursor-pointer",
              showDeviceDropdown
                ? "bg-muted text-foreground"
                : "text-muted-foreground hover:text-foreground hover:bg-muted/50"
            )}
            title="Select device for preview"
          >
            {selectedDevice ? (
              <Smartphone className="h-3 w-3" />
            ) : (
              <Monitor className="h-3 w-3" />
            )}
            <span className="hidden sm:inline">
              {selectedDevice ? `${selectedDevice.name} (${selectedDevice.width}×${selectedDevice.height})` : 'Responsive'}
            </span>
            <ChevronDown className={cn("h-2.5 w-2.5 transition-transform", showDeviceDropdown && "rotate-180")} />
          </button>

          {/* Device dropdown menu */}
          {showDeviceDropdown && (
            <div
              className="fixed z-[100] mt-1 w-56 rounded-xl border border-border/40 bg-background shadow-xl overflow-hidden"
              style={(() => {
                const rect = deviceDropdownRef.current?.getBoundingClientRect();
                if (!rect) return {};
                return { top: rect.bottom + 4, left: rect.left };
              })()}
            >
              <div className="max-h-72 overflow-y-auto py-1">
                {DEVICE_GROUPS.map((group) => {
                  const CategoryIcon = CATEGORY_ICONS[group.category];
                  return (
                    <div key={group.category}>
                      <div className="flex items-center gap-1.5 px-3 py-1.5 text-[10px] font-semibold text-muted-foreground/80 uppercase tracking-wider">
                        <CategoryIcon className="h-2.5 w-2.5" />
                        {CATEGORY_LABELS[group.category]}
                      </div>
                      {group.devices.map((device) => {
                        const isActive = selectedDevice
                          ? selectedDevice.name === device.name
                          : device.category === 'responsive';
                        return (
                          <button
                            key={device.name}
                            onClick={(e) => {
                              e.stopPropagation();
                              setSelectedDevice(device.category === 'responsive' ? null : device);
                              setShowDeviceDropdown(false);
                            }}
                            className={cn(
                              "flex items-center gap-2 w-full px-3 py-2 text-left text-xs transition-colors",
                              isActive
                                ? "bg-primary/10 text-primary font-semibold"
                                : "text-foreground hover:bg-muted/50"
                            )}
                          >
                            <span className="shrink-0 w-4 flex justify-center">
                              {isActive && <Check className="h-3 w-3" />}
                            </span>
                            <span className="truncate flex-1">{device.name}</span>
                            {device.category !== 'responsive' && (
                              <span className="shrink-0 text-[10px] text-muted-foreground font-mono">
                                {device.width}×{device.height}
                              </span>
                            )}
                          </button>
                        );
                      })}
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </div>

        {/* Reset to responsive button (when device is active) */}
        {selectedDevice && (
          <button
            onClick={() => setSelectedDevice(null)}
            className="ml-auto p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
            title="Reset to responsive"
          >
            <X className="h-3 w-3" />
          </button>
        )}
      </div>

      {/* ── Canvas area with device frame ──────────────────────────────────── */}
      <div className="flex-1 min-h-0 overflow-auto flex items-center justify-center bg-muted/10 p-1.5">
        <div
          className={cn(
            "relative shrink-0 overflow-hidden bg-background shadow-sm ring-1 ring-border/20 rounded",
            selectedDevice ? "shadow-2xl ring-border/30 rounded-lg" : "w-full h-full"
          )}
          style={selectedDevice ? { width: selectedDevice.width, height: selectedDevice.height } : undefined}
        >
          <iframe
            ref={iframeRef}
            srcDoc={srcDoc}
            className="w-full h-full border-none"
            sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
            onLoad={() => {
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
      </div>
    </div>
  );
}
