import { useState } from "preact/hooks";
import { edit, mediaImage } from "../api/client";
import type { EditResult, MediaView, PersonDetail } from "../api/types";
import { t } from "../i18n";
import { CropDialog } from "./cropper";
import { Icon } from "./icons";
import { Button, useAction } from "./ui";

/** Runs an edit and brings the page to the new state of the tree. */
export type Apply = (operation: () => Promise<EditResult>) => Promise<EditResult>;

/**
 * The pictures of a person, and which of them is the portrait. The portrait
 * is a choice: a new picture of the person, or the button on a row. A mark on
 * a photo of the gallery gives a picture and no portrait.
 */
export function PersonPictures({ detail, apply }: { detail: PersonDetail; apply: Apply }) {
  const action = useAction();
  const [cropping, setCropping] = useState<{ id: string; choose: boolean } | null>(null);
  const pictures = (detail.media ?? []).filter((item) => item.image);
  const portrait = detail.photo?.id ?? "";
  const cropped = pictures.find((item) => item.id === cropping?.id);
  const run = (operation: () => Promise<EditResult>): void => {
    void action.run(() => apply(operation).then(() => undefined));
  };
  const choose = (event: Event): void => {
    const input = event.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    input.value = "";
    if (file) {
      void action.run(async () => {
        const result = await apply(() => edit.addPhoto(detail.id, file));
        // The new picture is the portrait, so the person chooses its part now.
        setCropping(result.created ? { id: result.created, choose: false } : null);
      });
    }
  };
  const row = (item: MediaView) => {
    const chosen = item.id === portrait;
    return (
      <li class="photo-row" key={item.id}>
        <img src={mediaImage(item.id, "s", item.region)} alt={item.description ?? ""} loading="lazy" />
        <span class="row-text">
          <span>{chosen ? t("photoPortrait") : item.description || t("photoOther")}</span>
          {chosen && <small>{t("photoPortraitHelp")}</small>}
        </span>
        <span class="access-controls">
          {chosen ? (
            <Button kind="plain" busy={action.busy} onClick={() => run(() => edit.clearPortrait(detail.id))}>
              {t("photoClearPortrait")}
            </Button>
          ) : (
            <Button
              kind="plain"
              disabled={action.busy}
              onClick={() => setCropping({ id: item.id, choose: true })}
            >
              {t("photoMakePortrait")}
            </Button>
          )}
          {chosen && (
            <Button
              kind="plain"
              disabled={action.busy}
              onClick={() => setCropping({ id: item.id, choose: false })}
            >
              {t("photoCrop")}
            </Button>
          )}
          <Button
            kind="quiet-danger"
            busy={action.busy}
            onClick={() => run(() => edit.removeMedia(detail.id, item.id))}
          >
            {t("photoRemove")}
          </Button>
        </span>
      </li>
    );
  };
  return (
    <div class="photo-edit">
      {pictures.length > 0 && !portrait && <p class="row-empty">{t("photoNoPortrait")}</p>}
      {pictures.length > 0 && <ul class="photo-rows">{pictures.map(row)}</ul>}
      <div class="padded photo-add">
        <label class="button file-button">
          <Icon name="upload-simple" />
          {action.busy ? t("photoAdding") : t("photoAdd")}
          <input type="file" accept="image/jpeg,image/png" onChange={choose} disabled={action.busy} />
        </label>
        <p class="field-help">{t("photoHelp")}</p>
        {action.error && (
          <p class="field-error" role="alert">
            {action.error}
          </p>
        )}
      </div>
      {cropping && cropped && (
        <CropDialog
          key={cropped.id}
          media={cropped.id}
          region={cropped.region}
          onClose={() => setCropping(null)}
          onSave={async (region) => {
            await apply(() => edit.setPhoto(detail.id, cropped.id, region, cropping.choose));
            setCropping(null);
          }}
        />
      )}
    </div>
  );
}

/** The dialog with the pictures of a person, for the choice of the portrait. */
export function PortraitDialog({
  detail,
  apply,
  onClose,
}: {
  detail: PersonDetail;
  apply: Apply;
  onClose: () => void;
}) {
  return (
    <div class="modal-layer">
      <div class="modal portrait" role="dialog" aria-modal="true" aria-labelledby="portrait-title">
        <button type="button" class="sheet-close" aria-label={t("close")} onClick={onClose}>
          <Icon name="x" />
        </button>
        <h2 id="portrait-title">{t("portraitTitle")}</h2>
        <p class="lead">{t("portraitLead")}</p>
        <div class="group-body">
          <PersonPictures detail={detail} apply={apply} />
        </div>
        <div class="actions">
          <Button kind="filled" onClick={onClose}>
            {t("done")}
          </Button>
        </div>
      </div>
    </div>
  );
}
