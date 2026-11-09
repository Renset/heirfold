import { signal } from "@preact/signals";
import { useEffect, useState } from "preact/hooks";
import { ApiError, server } from "../api/client";
import { locale, t } from "../i18n";
import { me } from "../state";
import { CodeForm } from "../views/auth";
import { Icon } from "./icons";
import { Button, Spinner } from "./ui";

interface Pending {
  done: () => void;
  cancel: () => void;
}

const pending = signal<Pending | null>(null);

/**
 * Runs an action that changes who has access. The server accepts it only a
 * short time after a sign-in code. If the code is old, the page asks for a new
 * one and then runs the action again.
 */
export async function withFreshCode<T>(action: () => Promise<T>): Promise<T> {
  try {
    return await action();
  } catch (error) {
    if (!(error instanceof ApiError) || error.code !== "reauth_required") {
      throw error;
    }
  }
  await new Promise<void>((resolve, reject) => {
    pending.value = { done: resolve, cancel: () => reject(new ApiError(0, "cancelled")) };
  });
  return action();
}

/** The dialog that asks for a new sign-in code. The shell renders it once. */
export function ReauthDialog() {
  const request = pending.value;
  const email = me.value?.user.email ?? "";
  const [sent, setSent] = useState(false);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    if (!request) {
      setSent(false);
      setFailed(false);
      return;
    }
    server
      .requestCode(email, locale.value)
      .then(() => setSent(true))
      .catch(() => setFailed(true));
    const onKey = (event: KeyboardEvent): void => {
      if (event.key === "Escape") {
        pending.value = null;
        request.cancel();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [request, email]);

  if (!request) {
    return null;
  }
  const close = (): void => {
    pending.value = null;
    request.cancel();
  };
  return (
    <div class="modal-layer">
      <div class="modal" role="dialog" aria-modal="true" aria-labelledby="reauth-title">
        <button type="button" class="sheet-close" aria-label={t("close")} onClick={close}>
          <Icon name="x" />
        </button>
        <h2 id="reauth-title">{t("reauthTitle")}</h2>
        <p class="lead">{t("reauthLead")}</p>
        {!sent && !failed && <Spinner />}
        {failed && (
          <>
            <p class="field-error" role="alert">
              {t("errorRateLimited")}
            </p>
            <Button onClick={close}>{t("close")}</Button>
          </>
        )}
        {sent && (
          <CodeForm
            email={email}
            onDone={(account) => {
              me.value = account;
              pending.value = null;
              request.done();
            }}
          />
        )}
      </div>
    </div>
  );
}
