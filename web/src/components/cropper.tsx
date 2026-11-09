import type { ComponentChildren } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { mediaImage, mediaOrientation } from "../api/client";
import type { Region } from "../api/types";
import { t } from "../i18n";
import {
  type Box,
  boxHeight,
  boxRegion,
  type Corner,
  maxWidth,
  moveBox,
  resizeBox,
  scaleBox,
  startBox,
} from "../lib/crop";
import { toDisplay, toStored } from "../lib/orient";
import { Icon } from "./icons";
import { Button, Spinner, useAction } from "./ui";

const corners: Corner[] = ["nw", "ne", "sw", "se"];

interface Drag {
  pointer: number;
  corner?: Corner;
  startX: number;
  startY: number;
  box: Box;
}

/**
 * The dialog that chooses a square part of a picture: the portrait of a
 * person, or the face of a person on a photo of the gallery. The file does
 * not change: the tree keeps the part as numbers.
 */
export function CropDialog({
  media,
  region,
  face,
  children,
  ready = true,
  onSave,
  onClose,
}: {
  media: string;
  region?: Region;
  /** The dialog marks a face on a photo. It has other words and no "whole picture" choice. */
  face?: boolean;
  /** More fields of the dialog, above the buttons. */
  children?: ComponentChildren;
  /** False while the other fields of the dialog are not complete. */
  ready?: boolean;
  /** Stores the part. Null shows the whole picture. */
  onSave: (region: Region | null) => Promise<void>;
  onClose: () => void;
}) {
  const [aspect, setAspect] = useState(0);
  const [failed, setFailed] = useState(false);
  const [box, setBox] = useState<Box | null>(null);
  // The stored part is on the pixels of the file. The dialog shows the upright picture.
  const [orientation, setOrientation] = useState<number | null>(null);
  const image = useRef<HTMLImageElement>(null);
  const frame = useRef<HTMLButtonElement>(null);
  const drag = useRef<Drag | null>(null);
  const action = useAction();

  useEffect(() => {
    const onKey = (event: KeyboardEvent): void => {
      if (event.key === "Escape") {
        onClose();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  useEffect(() => {
    if (box) {
      frame.current?.focus();
    }
  }, [box !== null]);

  useEffect(() => {
    let active = true;
    void mediaOrientation(media).then((value) => {
      if (active) {
        setOrientation(value);
      }
    });
    return () => {
      active = false;
    };
  }, [media]);

  const loaded = (): void => {
    const element = image.current;
    if (!element?.naturalWidth || !element.naturalHeight) {
      setFailed(true);
      return;
    }
    setAspect(element.naturalWidth / element.naturalHeight);
  };

  // The frame appears when the picture and its orientation are known.
  useEffect(() => {
    if (aspect && orientation !== null && !box) {
      setBox(startBox(aspect, region ? toDisplay(region, orientation) : undefined));
    }
  }, [aspect, orientation]);

  /** Returns the pointer position as a part of the picture. */
  const point = (event: PointerEvent): [number, number] => {
    const rect = image.current?.getBoundingClientRect();
    if (!rect?.width || !rect.height) {
      return [0, 0];
    }
    return [(event.clientX - rect.left) / rect.width, (event.clientY - rect.top) / rect.height];
  };

  const start = (event: PointerEvent, corner?: Corner): void => {
    if (!box || event.button > 0) {
      return;
    }
    event.preventDefault();
    event.stopPropagation();
    const [x, y] = point(event);
    drag.current = { pointer: event.pointerId, corner, startX: x, startY: y, box };
    frame.current?.setPointerCapture(event.pointerId);
    frame.current?.focus();
  };

  const move = (event: PointerEvent): void => {
    const state = drag.current;
    if (!state || state.pointer !== event.pointerId) {
      return;
    }
    const [x, y] = point(event);
    setBox(
      state.corner
        ? resizeBox(state.box, state.corner, x, y, aspect)
        : moveBox(state.box, x - state.startX, y - state.startY, aspect),
    );
  };

  const stop = (event: PointerEvent): void => {
    if (drag.current?.pointer === event.pointerId) {
      drag.current = null;
    }
  };

  const onKeyDown = (event: KeyboardEvent): void => {
    if (!box) {
      return;
    }
    const step = event.shiftKey ? 0.05 : 0.01;
    const moves: Record<string, [number, number]> = {
      ArrowLeft: [-step, 0],
      ArrowRight: [step, 0],
      ArrowUp: [0, -step],
      ArrowDown: [0, step],
    };
    const delta = moves[event.key];
    if (delta) {
      event.preventDefault();
      setBox(moveBox(box, delta[0], delta[1], aspect));
    } else if (event.key === "+" || event.key === "=") {
      event.preventDefault();
      setBox(scaleBox(box, box.w + step, aspect));
    } else if (event.key === "-") {
      event.preventDefault();
      setBox(scaleBox(box, box.w - step, aspect));
    }
  };

  const percent = (value: number): string => `${(value * 100).toFixed(3)}%`;
  const widest = aspect ? maxWidth(aspect) : 1;

  return (
    <div class="modal-layer">
      <div class="modal crop" role="dialog" aria-modal="true" aria-labelledby="crop-title">
        <button type="button" class="sheet-close" aria-label={t("close")} onClick={onClose}>
          <Icon name="x" />
        </button>
        <h2 id="crop-title">{t(face ? "faceTitle" : "cropTitle")}</h2>
        <p class="lead">{t(face ? "faceLead" : "cropLead")}</p>
        {failed ? (
          <p class="field-error" role="alert">
            {t("cropFailed")}
          </p>
        ) : (
          <div class="crop-stage">
            {!box && <Spinner />}
            <div class="crop-picture">
              <img
                ref={image}
                src={mediaImage(media, "l")}
                alt=""
                draggable={false}
                onLoad={loaded}
                onError={() => setFailed(true)}
              />
              {box && (
                <button
                  type="button"
                  ref={frame}
                  class="crop-frame"
                  aria-label={t("cropFrame")}
                  style={{
                    left: percent(box.x),
                    top: percent(box.y),
                    width: percent(box.w),
                    height: percent(boxHeight(box, aspect)),
                  }}
                  onPointerDown={(event) => start(event)}
                  onPointerMove={move}
                  onPointerUp={stop}
                  onPointerCancel={stop}
                  onKeyDown={onKeyDown}
                >
                  {corners.map((corner) => (
                    <span
                      key={corner}
                      class={`crop-handle ${corner}`}
                      onPointerDown={(event) => start(event, corner)}
                    />
                  ))}
                </button>
              )}
            </div>
          </div>
        )}
        {box && !failed && (
          <label class="crop-size">
            <span>{t("cropSize")}</span>
            <input
              type="range"
              min={10}
              max={100}
              step={1}
              value={Math.round((box.w / widest) * 100)}
              onInput={(event) =>
                setBox(scaleBox(box, (Number(event.currentTarget.value) / 100) * widest, aspect))
              }
            />
          </label>
        )}
        {children}
        {action.error && (
          <p class="field-error" role="alert">
            {action.error}
          </p>
        )}
        <div class="actions">
          <Button
            kind="filled"
            busy={action.busy}
            disabled={!box || failed || !ready}
            onClick={() => {
              if (box) {
                void action.run(() => onSave(toStored(boxRegion(box, aspect), orientation ?? 1)));
              }
            }}
          >
            {t("save")}
          </Button>
          {region && !face && (
            <Button busy={action.busy} onClick={() => void action.run(() => onSave(null))}>
              {t("cropWhole")}
            </Button>
          )}
          <Button kind="plain" onClick={onClose}>
            {t("cancel")}
          </Button>
        </div>
      </div>
    </div>
  );
}
