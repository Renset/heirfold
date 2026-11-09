import type { ComponentChildren } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { t } from "../i18n";
import { Icon } from "./icons";

const DECELERATION = 0.99;

/**
 * A panel that rises from the bottom edge. A user can drag it down to close it:
 * the panel tracks the pointer 1:1, and the release velocity decides the result.
 */
export function Sheet({ children, onClose }: { children: ComponentChildren; onClose: () => void }) {
  const panel = useRef<HTMLDivElement>(null);
  const [shown, setShown] = useState(false);
  const drag = useRef<{ startY: number; y: number; lastY: number; lastT: number; velocity: number } | null>(
    null,
  );

  useEffect(() => {
    const timer = setTimeout(() => setShown(true), 16);
    const onKey = (event: KeyboardEvent): void => {
      if (event.key === "Escape") {
        onClose();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => {
      clearTimeout(timer);
      document.removeEventListener("keydown", onKey);
    };
  }, [onClose]);

  const onDown = (event: PointerEvent): void => {
    const el = panel.current;
    if (!el) {
      return;
    }
    el.setPointerCapture(event.pointerId);
    el.classList.add("dragging");
    drag.current = { startY: event.clientY, y: 0, lastY: event.clientY, lastT: event.timeStamp, velocity: 0 };
  };

  const onMove = (event: PointerEvent): void => {
    const state = drag.current;
    const el = panel.current;
    if (!state || !el) {
      return;
    }
    const raw = event.clientY - state.startY;
    // Above the resting position the panel resists the pointer.
    state.y = raw >= 0 ? raw : raw * 0.2;
    const dt = event.timeStamp - state.lastT;
    if (dt > 0) {
      state.velocity = ((event.clientY - state.lastY) / dt) * 1000;
    }
    state.lastY = event.clientY;
    state.lastT = event.timeStamp;
    el.style.setProperty("--drag", `${state.y}px`);
  };

  const onUp = (): void => {
    const state = drag.current;
    const el = panel.current;
    if (!state || !el) {
      return;
    }
    drag.current = null;
    el.classList.remove("dragging");
    const projected = state.y + ((state.velocity / 1000) * DECELERATION) / (1 - DECELERATION);
    el.style.setProperty("--drag", "0px");
    if (projected > el.getBoundingClientRect().height / 2) {
      onClose();
    }
  };

  return (
    <div class={shown ? "sheet-layer shown" : "sheet-layer"}>
      <div class="sheet" ref={panel} role="dialog" aria-modal="false">
        <div
          class="sheet-grabber"
          onPointerDown={onDown}
          onPointerMove={onMove}
          onPointerUp={onUp}
          onPointerCancel={onUp}
        >
          <span />
        </div>
        <button type="button" class="sheet-close" aria-label={t("close")} onClick={onClose}>
          <Icon name="x" />
        </button>
        {children}
      </div>
    </div>
  );
}
