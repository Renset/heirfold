import type { ComponentChildren } from "preact";
import { useState } from "preact/hooks";
import { ApiError, mediaImage } from "../api/client";
import type { Card } from "../api/types";
import { locale, t } from "../i18n";
import { initials, nameLines } from "../lib/format";
import { Icon, type IconName } from "./icons";

export function Spinner() {
  return <div class="spinner" role="status" aria-label={t("loading")} />;
}

export function Button({
  children,
  kind = "tinted",
  type = "button",
  disabled,
  busy,
  onClick,
}: {
  children: ComponentChildren;
  /** "quiet-danger" is a text button for an action that takes something away. */
  kind?: "filled" | "tinted" | "plain" | "danger" | "quiet-danger";
  type?: "button" | "submit";
  disabled?: boolean;
  busy?: boolean;
  onClick?: () => void;
}) {
  return (
    <button
      type={type}
      class={`button ${kind === "quiet-danger" ? "plain quiet-danger" : kind}`}
      disabled={disabled || busy}
      aria-busy={busy}
      onClick={onClick}
    >
      {children}
    </button>
  );
}

export function EmptyState({
  icon,
  title,
  children,
}: {
  icon: IconName;
  title: string;
  children?: ComponentChildren;
}) {
  return (
    <div class="empty">
      <Icon name={icon} />
      <p class="empty-title">{title}</p>
      {children}
    </div>
  );
}

export function Avatar({ card, size = "m" }: { card: Card; size?: "s" | "m" | "l" }) {
  const [failed, setFailed] = useState("");
  const src = card.photo ? mediaImage(card.photo.id, size === "s" ? "s" : "m", card.photo.region) : "";
  return (
    <span class={`avatar ${size}`} aria-hidden="true">
      {src && failed !== src ? (
        <img src={src} alt="" loading="lazy" decoding="async" onError={() => setFailed(src)} />
      ) : (
        initials(card)
      )}
    </span>
  );
}

export function displayName(card: Card): { main: string; second: string } {
  const lines = nameLines(card, locale.value);
  if (!lines.main && !lines.second) {
    return { main: t("unnamed"), second: "" };
  }
  return lines;
}

export function Field({
  label,
  help,
  error,
  children,
}: {
  label: string;
  help?: string;
  error?: string | null;
  children: ComponentChildren;
}) {
  return (
    <label class="field">
      <span class="field-label">{label}</span>
      {children}
      {help && !error && <span class="field-help">{help}</span>}
      {error && (
        <span class="field-error" role="alert">
          {error}
        </span>
      )}
    </label>
  );
}

export function errorText(error: unknown): string {
  if (!(error instanceof ApiError)) {
    return t("errorGeneric");
  }
  switch (error.code) {
    case "network":
      return t("errorNetwork");
    case "invalid_code":
      return t("errorInvalidCode");
    case "rate_limited":
      return t("errorRateLimited");
    case "bad_email":
      return t("errorBadEmail");
    case "email_taken":
      return t("errorEmailTaken");
    case "bad_name":
      return t("errorBadName");
    case "bad_person":
      return t("errorBadPerson");
    case "outside_branch":
      return t("errorOutsideBranch");
    case "person_locked":
      return t("errorPersonLocked");
    case "needs_person":
      return t("errorNeedsPerson");
    case "own_account":
      return t("errorOwnAccount");
    case "managed_by_gramps":
      return t("errorManagedByGramps");
    case "storage_full":
      return t("errorStorageFull");
    case "bad_image":
      return t("errorBadImage");
    case "media_off":
      return t("errorMediaOff");
    case "changed":
      return t("errorChanged");
    case "invalid":
      return t("errorInvalid");
    case "parents_full":
      return t("errorParentsFull");
    case "loop":
      return t("errorLoop");
    case "not_found":
      return t("errorNotFound");
    case "setup_denied":
      return t("errorSetupDenied");
    case "too_large":
      return t("errorTooLarge");
    case "not_gramps":
      return t("errorNotGramps");
    case "import_busy":
      return t("errorImportBusy");
    case "import_expired":
      return t("errorImportExpired");
    case "tree_exists":
      return t("errorTreeExists");
    case "last_admin":
      return t("errorLastAdmin");
    case "forbidden":
      return t("errorForbidden");
    default:
      return t("errorGeneric");
  }
}

/** Runs a form action and tracks its busy and error state. */
export function useAction(): {
  busy: boolean;
  error: string | null;
  run: (action: () => Promise<void>) => Promise<void>;
  clear: () => void;
} {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  return {
    busy,
    error,
    clear: () => setError(null),
    run: async (action) => {
      setBusy(true);
      setError(null);
      try {
        await action();
      } catch (caught) {
        // The person closed the dialog that asks for a code. That is no error.
        if (caught instanceof ApiError && caught.code === "cancelled") {
          return;
        }
        setError(errorText(caught));
      } finally {
        setBusy(false);
      }
    },
  };
}
