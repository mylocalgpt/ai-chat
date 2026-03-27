import { useCallback, useEffect, useRef } from "react";

/**
 * Manages auto-scroll behavior for a scrollable container.
 * Scrolls to bottom when new content arrives, unless the user has scrolled up.
 * Resumes auto-scroll when the user scrolls back to the bottom.
 */
export function useAutoScroll(deps: unknown[]) {
  const containerRef = useRef<HTMLDivElement>(null);
  const isLockedRef = useRef(true);

  const scrollToBottom = useCallback(() => {
    const el = containerRef.current;
    if (el) {
      el.scrollTop = el.scrollHeight;
    }
  }, []);

  // Check if user has scrolled away from bottom.
  const handleScroll = useCallback(() => {
    const el = containerRef.current;
    if (!el) return;

    const threshold = 40; // px from bottom
    const atBottom =
      el.scrollHeight - el.scrollTop - el.clientHeight < threshold;
    isLockedRef.current = atBottom;
  }, []);

  // Auto-scroll when dependencies change and user hasn't scrolled up.
  useEffect(() => {
    if (isLockedRef.current) {
      scrollToBottom();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  return { containerRef, handleScroll, scrollToBottom };
}
