import { cn } from "cn";
import {
  useEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type PointerEvent,
  type ReactNode,
} from "react";
import { useIsMobile } from "@/hooks/use-mobile";
import { usePersistentState } from "@/hooks/use-persistent-state";

const minHeight = 160;

/**
 * The torrent list with the open torrent's details below it, split by a
 * draggable handle. At phone width the details cover the list instead.
 */
export function DetailSplit({
  open,
  detail,
  children,
}: {
  open: boolean;
  detail: ReactNode;
  children: ReactNode;
}) {
  const isMobile = useIsMobile();
  const containerRef = useRef<HTMLDivElement>(null);
  const [height, setHeight] = usePersistentState<number>(
    "piecemeal.detail.height",
    360,
    (v): v is number => typeof v === "number" && Number.isFinite(v),
  );

  const [containerHeight, setContainerHeight] = useState(800);
  useEffect(() => {
    const el = containerRef.current;
    if (!el) {
      return;
    }
    const ro = new ResizeObserver(([entry]) =>
      setContainerHeight(entry?.contentRect.height ?? 800),
    );
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  // The list keeps at least 120px.
  const clamp = (h: number) => Math.round(Math.max(minHeight, Math.min(containerHeight - 120, h)));

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    e.preventDefault();
    const startY = e.clientY;
    const startH = height;
    const target = e.currentTarget;
    target.setPointerCapture(e.pointerId);
    const onMove = (ev: globalThis.PointerEvent) => setHeight(clamp(startH + startY - ev.clientY));
    const onUp = () => {
      target.removeEventListener("pointermove", onMove);
      target.removeEventListener("pointerup", onUp);
      target.removeEventListener("pointercancel", onUp);
    };
    target.addEventListener("pointermove", onMove);
    target.addEventListener("pointerup", onUp);
    target.addEventListener("pointercancel", onUp);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const step = e.shiftKey ? 80 : 24;
    if (e.key === "ArrowUp") {
      e.preventDefault();
      setHeight(clamp(height + step));
    } else if (e.key === "ArrowDown") {
      e.preventDefault();
      setHeight(clamp(height - step));
    }
  };

  return (
    <div ref={containerRef} className="flex min-h-0 flex-1 flex-col">
      <div className="flex min-h-0 flex-1 flex-col">{children}</div>
      {open &&
        (isMobile ? (
          <div className="fixed inset-0 z-40 flex flex-col bg-background">{detail}</div>
        ) : (
          <>
            <div
              role="separator"
              aria-orientation="horizontal"
              aria-label="Resize details"
              aria-valuenow={height}
              aria-valuemin={minHeight}
              tabIndex={0}
              onPointerDown={onPointerDown}
              onKeyDown={onKeyDown}
              className={cn(
                "relative h-1.5 shrink-0 cursor-row-resize touch-none border-t bg-muted/40 hover:bg-ring/30 focus-visible:bg-ring/40 focus-visible:outline-none",
              )}
            />
            <section
              aria-label="Torrent details"
              className="flex shrink-0 flex-col overflow-hidden bg-background"
              style={{ height: clamp(height) }}
            >
              {detail}
            </section>
          </>
        ))}
    </div>
  );
}
