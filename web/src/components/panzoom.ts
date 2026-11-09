import type { RefObject } from "preact";
import { useEffect, useRef } from "preact/hooks";

// Pan and zoom for a chart. The motion follows the fluid-interface rules:
// a drag tracks the pointer 1:1, a release hands its velocity to a critically
// damped spring, and every animation can be interrupted from its current value.

export interface Bounds {
  minX: number;
  minY: number;
  maxX: number;
  maxY: number;
}

interface Spring {
  value: number;
  velocity: number;
  target: number;
}

const RESPONSE = 0.42;
const OMEGA = (2 * Math.PI) / RESPONSE;
const DECELERATION = 0.994;
const MIN_SCALE = 0.25;
const MAX_SCALE = 2.2;
const DRAG_THRESHOLD = 6;
const PADDING = 28;
const PHONE_WIDTH = 544;
// People on phones must read a name without zooming first.
const PHONE_SCALE = 1.15;

function stepSpring(spring: Spring, dt: number): void {
  const offset = spring.value - spring.target;
  const decay = Math.exp(-OMEGA * dt);
  const term = spring.velocity + OMEGA * offset;
  spring.value = spring.target + (offset + term * dt) * decay;
  spring.velocity = (term - OMEGA * (offset + term * dt)) * decay;
}

function settled(spring: Spring, epsilon: number): boolean {
  return Math.abs(spring.value - spring.target) < epsilon && Math.abs(spring.velocity) < epsilon * 10;
}

function clamp(value: number, low: number, high: number): number {
  return Math.min(Math.max(value, low), high);
}

export interface PanZoomControls {
  zoomBy: (factor: number) => void;
  fit: () => void;
  /** Moves a point of the chart to the middle of the view, at a size that people can read. */
  focusOn: (x: number, y: number) => void;
  wasDragged: () => boolean;
}

/**
 * A chart shows less in each card when the reader zooms out, so the text that
 * stays is large enough to read. The chart gets a class for the level:
 * nothing for the full card, "mid" for the name in large letters, "low" for
 * the given name only, "far" for a mark without text.
 */
export type Detail = "" | "mid" | "low" | "far";

export function detailAt(scale: number): Detail {
  if (scale < 0.14) {
    return "far";
  }
  if (scale < 0.25) {
    return "low";
  }
  return scale < 0.7 ? "mid" : "";
}

export function usePanZoom(
  svgRef: RefObject<SVGSVGElement | null>,
  layerRef: RefObject<SVGGElement | null>,
  bounds: Bounds,
  focus: { x: number; y: number },
  anchorY = 0.5,
  /** Open with the whole content in view. A photo needs this, a chart does not. */
  startWhole = false,
): { current: PanZoomControls } {
  const controls = useRef<PanZoomControls>({
    zoomBy: () => undefined,
    fit: () => undefined,
    focusOn: () => undefined,
    wasDragged: () => false,
  });
  const key = `${bounds.minX},${bounds.minY},${bounds.maxX},${bounds.maxY},${focus.x},${focus.y}`;

  useEffect(() => {
    const svg = svgRef.current;
    const layer = layerRef.current;
    if (!svg || !layer) {
      return;
    }
    const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;
    const tx: Spring = { value: 0, velocity: 0, target: 0 };
    const ty: Spring = { value: 0, velocity: 0, target: 0 };
    const scale: Spring = { value: 1, velocity: 0, target: 1 };
    let frame = 0;
    let last = 0;
    let dragged = false;

    let detail: Detail | null = null;
    const paint = (): void => {
      layer.setAttribute(
        "transform",
        `translate(${tx.value.toFixed(2)} ${ty.value.toFixed(2)}) scale(${scale.value.toFixed(4)})`,
      );
      const next = detailAt(scale.value);
      if (next !== detail) {
        detail = next;
        for (const name of ["mid", "low", "far"]) {
          svg.classList.toggle(name, name === next);
        }
      }
    };

    const tick = (now: number): void => {
      const dt = Math.min((now - last) / 1000, 1 / 30);
      last = now;
      stepSpring(tx, dt);
      stepSpring(ty, dt);
      stepSpring(scale, dt);
      paint();
      if (settled(tx, 0.1) && settled(ty, 0.1) && settled(scale, 0.0005)) {
        tx.value = tx.target;
        ty.value = ty.target;
        scale.value = scale.target;
        tx.velocity = ty.velocity = scale.velocity = 0;
        paint();
        frame = 0;
        return;
      }
      frame = requestAnimationFrame(tick);
    };

    const animate = (): void => {
      if (reduced) {
        tx.value = tx.target;
        ty.value = ty.target;
        scale.value = scale.target;
        tx.velocity = ty.velocity = scale.velocity = 0;
        paint();
        return;
      }
      if (!frame) {
        last = performance.now();
        frame = requestAnimationFrame(tick);
      }
    };

    const stop = (): void => {
      if (frame) {
        cancelAnimationFrame(frame);
        frame = 0;
      }
    };

    const size = (): { width: number; height: number } => {
      const rect = svg.getBoundingClientRect();
      return { width: rect.width, height: rect.height };
    };

    /** Keeps some of the chart on screen. */
    const limitTargets = (): void => {
      const { width, height } = size();
      const s = scale.target;
      const margin = 80;
      tx.target = clamp(tx.target, margin - bounds.maxX * s, width - margin - bounds.minX * s);
      ty.target = clamp(ty.target, margin - bounds.maxY * s, height - margin - bounds.minY * s);
    };

    /** The scale that shows all of the content. */
    const wholeScale = (): number => {
      const { width, height } = size();
      return Math.min(
        (width - PADDING * 2) / (bounds.maxX - bounds.minX),
        (height - PADDING * 2) / (bounds.maxY - bounds.minY),
      );
    };
    /** A photo can be larger than a chart, so it may shrink until it fits. */
    const lowest = (): number => (startWhole ? Math.min(MIN_SCALE, wholeScale()) : MIN_SCALE);

    /** Shows the whole chart. Only the fit button does this. */
    const fit = (): void => {
      const { width, height } = size();
      const s = clamp(wholeScale(), lowest(), 1);
      scale.target = s;
      tx.target = width / 2 - ((bounds.minX + bounds.maxX) / 2) * s;
      ty.target = height / 2 - ((bounds.minY + bounds.maxY) / 2) * s;
      animate();
    };

    /** Opens the chart at a readable size, around the main person. */
    const open = (): void => {
      const { width, height } = size();
      const boundsW = bounds.maxX - bounds.minX;
      const boundsH = bounds.maxY - bounds.minY;
      const whole = clamp(wholeScale(), lowest(), 1);
      const s = startWhole ? whole : width < PHONE_WIDTH ? PHONE_SCALE : 1;
      scale.value = scale.target = s;
      if (boundsW * s <= width - PADDING * 2 && boundsH * s <= height - PADDING * 2) {
        tx.value = tx.target = width / 2 - ((bounds.minX + bounds.maxX) / 2) * s;
        ty.value = ty.target = height / 2 - ((bounds.minY + bounds.maxY) / 2) * s;
      } else {
        tx.value = tx.target = width / 2 - focus.x * s;
        ty.value = ty.target = height * anchorY - focus.y * s;
      }
      tx.velocity = ty.velocity = scale.velocity = 0;
      paint();
    };

    const zoomAt = (factor: number, cx: number, cy: number, spring: boolean): void => {
      const before = spring ? scale.target : scale.value;
      const next = clamp(before * factor, lowest(), MAX_SCALE);
      const ratio = next / before;
      if (spring) {
        tx.target = cx - (cx - tx.target) * ratio;
        ty.target = cy - (cy - ty.target) * ratio;
        scale.target = next;
        animate();
      } else {
        tx.value = tx.target = cx - (cx - tx.value) * ratio;
        ty.value = ty.target = cy - (cy - ty.value) * ratio;
        scale.value = scale.target = next;
        paint();
      }
    };

    const pointers = new Map<number, { x: number; y: number }>();
    let start = { x: 0, y: 0, tx: 0, ty: 0 };
    let pinch = 0;
    let history: { x: number; y: number; t: number }[] = [];

    const local = (event: PointerEvent | WheelEvent): { x: number; y: number } => {
      const rect = svg.getBoundingClientRect();
      return { x: event.clientX - rect.left, y: event.clientY - rect.top };
    };

    const onDown = (event: PointerEvent): void => {
      if (event.button !== 0) {
        return;
      }
      stop();
      tx.target = tx.value;
      ty.target = ty.value;
      tx.velocity = ty.velocity = scale.velocity = 0;
      scale.target = scale.value;
      const p = local(event);
      pointers.set(event.pointerId, p);
      dragged = false;
      start = { x: p.x, y: p.y, tx: tx.value, ty: ty.value };
      history = [{ ...p, t: event.timeStamp }];
      if (pointers.size === 2) {
        const [a, b] = [...pointers.values()];
        if (a && b) {
          pinch = Math.hypot(a.x - b.x, a.y - b.y);
          dragged = true;
        }
      }
    };

    const onMove = (event: PointerEvent): void => {
      if (!pointers.has(event.pointerId)) {
        return;
      }
      const p = local(event);
      pointers.set(event.pointerId, p);
      if (pointers.size === 2) {
        const [a, b] = [...pointers.values()];
        if (a && b) {
          const distance = Math.hypot(a.x - b.x, a.y - b.y);
          if (pinch > 0) {
            zoomAt(distance / pinch, (a.x + b.x) / 2, (a.y + b.y) / 2, false);
          }
          pinch = distance;
        }
        return;
      }
      if (!dragged && Math.hypot(p.x - start.x, p.y - start.y) < DRAG_THRESHOLD) {
        return;
      }
      if (!dragged) {
        dragged = true;
        svg.setPointerCapture(event.pointerId);
        svg.classList.add("dragging");
        // Continue from the current position, with no jump at the threshold.
        start = { x: p.x, y: p.y, tx: tx.value, ty: ty.value };
      }
      tx.value = tx.target = start.tx + p.x - start.x;
      ty.value = ty.target = start.ty + p.y - start.y;
      history.push({ ...p, t: event.timeStamp });
      if (history.length > 6) {
        history.shift();
      }
      paint();
    };

    const onUp = (event: PointerEvent): void => {
      if (!pointers.delete(event.pointerId)) {
        return;
      }
      svg.classList.remove("dragging");
      if (pointers.size === 1) {
        const [rest] = [...pointers.values()];
        if (rest) {
          start = { x: rest.x, y: rest.y, tx: tx.value, ty: ty.value };
        }
        return;
      }
      if (pointers.size > 0 || !dragged) {
        return;
      }
      const first = history[0];
      const end = history[history.length - 1];
      if (first && end && end.t > first.t) {
        const seconds = (end.t - first.t) / 1000;
        const vx = (end.x - first.x) / seconds;
        const vy = (end.y - first.y) / seconds;
        const project = (v: number): number => ((v / 1000) * DECELERATION) / (1 - DECELERATION);
        tx.target = tx.value + project(vx);
        ty.target = ty.value + project(vy);
        tx.velocity = vx;
        ty.velocity = vy;
      }
      limitTargets();
      animate();
    };

    const onWheel = (event: WheelEvent): void => {
      event.preventDefault();
      stop();
      const p = local(event);
      const isMouseWheel = event.deltaMode !== 0 || (event.deltaX === 0 && Math.abs(event.deltaY) >= 100);
      if (event.ctrlKey || event.metaKey || isMouseWheel) {
        const amount = event.ctrlKey ? -event.deltaY * 0.01 : -Math.sign(event.deltaY) * 0.12;
        zoomAt(Math.exp(amount), p.x, p.y, false);
      } else {
        tx.value = tx.target = tx.value - event.deltaX;
        ty.value = ty.target = ty.value - event.deltaY;
        paint();
      }
    };

    const onKey = (event: KeyboardEvent): void => {
      const step = 80;
      const moves: Record<string, [number, number]> = {
        ArrowLeft: [step, 0],
        ArrowRight: [-step, 0],
        ArrowUp: [0, step],
        ArrowDown: [0, -step],
      };
      const move = moves[event.key];
      if (move) {
        event.preventDefault();
        tx.target += move[0];
        ty.target += move[1];
        limitTargets();
        animate();
      } else if (event.key === "+" || event.key === "=") {
        const { width, height } = size();
        zoomAt(1.25, width / 2, height / 2, true);
      } else if (event.key === "-") {
        const { width, height } = size();
        zoomAt(0.8, width / 2, height / 2, true);
      } else if (event.key === "0") {
        fit();
      }
    };

    controls.current = {
      zoomBy: (factor) => {
        const { width, height } = size();
        zoomAt(factor, width / 2, height / 2, true);
      },
      fit,
      focusOn: (x, y) => {
        const { width, height } = size();
        const s = Math.max(scale.target, width < PHONE_WIDTH ? PHONE_SCALE : 1);
        scale.target = s;
        tx.target = width / 2 - x * s;
        ty.target = height / 2 - y * s;
        animate();
      },
      wasDragged: () => dragged,
    };

    open();
    svg.addEventListener("pointerdown", onDown);
    svg.addEventListener("pointermove", onMove);
    svg.addEventListener("pointerup", onUp);
    svg.addEventListener("pointercancel", onUp);
    svg.addEventListener("wheel", onWheel, { passive: false });
    svg.addEventListener("keydown", onKey);
    return () => {
      stop();
      svg.removeEventListener("pointerdown", onDown);
      svg.removeEventListener("pointermove", onMove);
      svg.removeEventListener("pointerup", onUp);
      svg.removeEventListener("pointercancel", onUp);
      svg.removeEventListener("wheel", onWheel);
      svg.removeEventListener("keydown", onKey);
    };
  }, [key]);

  return controls;
}
