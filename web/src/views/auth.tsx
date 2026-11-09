import type { ComponentChildren } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { server } from "../api/client";
import type { Me } from "../api/types";
import { Button, Field, useAction } from "../components/ui";
import { locale, t } from "../i18n";
import { afterSignIn, codesInLog, navigate } from "../state";

function AuthCard({ title, lead, children }: { title: string; lead?: string; children?: ComponentChildren }) {
  return (
    <div class="auth">
      <div class="auth-card">
        <img src="icons/icon.svg" alt="" width="64" height="64" />
        <h1>{title}</h1>
        {lead && <p class="auth-lead">{lead}</p>}
        {children}
      </div>
    </div>
  );
}

/** How long a person waits before the page offers a second code. */
const RESEND_SECONDS = 30;
const CODE_DIGITS = 8;

/**
 * Asks for the code that the server sent, and signs the person in. Both the
 * sign-in page and the check before a sensitive action use it.
 */
export function CodeForm({
  email,
  onDone,
  onBack,
}: {
  email: string;
  onDone: (account: Me) => void | Promise<void>;
  onBack?: () => void;
}) {
  const [code, setCode] = useState("");
  const [wait, setWait] = useState(RESEND_SECONDS);
  const verify = useAction();
  const again = useAction();
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    input.current?.focus();
  }, []);

  useEffect(() => {
    if (wait <= 0) {
      return;
    }
    const timer = setTimeout(() => setWait(wait - 1), 1000);
    return () => clearTimeout(timer);
  }, [wait]);

  const submit = (value: string): void => {
    void verify.run(async () => {
      await onDone(await server.verifyCode(value));
    });
  };

  const onInput = (value: string): void => {
    setCode(value);
    // A complete code needs no extra tap. A paste from the letter works too.
    if (value.replace(/\D/g, "").length === CODE_DIGITS && !verify.busy) {
      submit(value);
    }
  };

  return (
    <form
      class="form"
      onSubmit={(event) => {
        event.preventDefault();
        submit(code);
      }}
    >
      <p class="auth-note">{codesInLog.value ? t("codeInLog") : t("codeSent", { email })}</p>
      <Field label={t("codeLabel")} help={t("codeHelp")} error={verify.error ?? again.error}>
        <input
          ref={input}
          class="code-input"
          inputMode="numeric"
          autocomplete="one-time-code"
          maxLength={CODE_DIGITS + 3}
          value={code}
          onInput={(event) => onInput(event.currentTarget.value)}
          required
        />
      </Field>
      <Button kind="filled" type="submit" busy={verify.busy}>
        {t("signIn")}
      </Button>
      <div class="auth-links">
        <Button
          kind="plain"
          disabled={wait > 0}
          busy={again.busy}
          onClick={() =>
            void again.run(async () => {
              await server.requestCode(email, locale.value);
              setCode("");
              verify.clear();
              setWait(RESEND_SECONDS);
              input.current?.focus();
            })
          }
        >
          {wait > 0 ? t("codeAgainIn", { seconds: wait }) : t("codeAgain")}
        </Button>
        {onBack && (
          <Button kind="plain" onClick={onBack}>
            {t("codeOtherEmail")}
          </Button>
        )}
      </div>
    </form>
  );
}

export function LoginView() {
  const [email, setEmail] = useState("");
  const [sentTo, setSentTo] = useState<string | null>(null);
  const action = useAction();

  if (sentTo) {
    return (
      <AuthCard title={t("codeTitle")}>
        <CodeForm email={sentTo} onDone={afterSignIn} onBack={() => setSentTo(null)} />
      </AuthCard>
    );
  }
  return (
    <AuthCard title={t("appName")} lead={t("signInLead")}>
      <form
        class="form"
        onSubmit={(event) => {
          event.preventDefault();
          const address = email.trim();
          void action.run(async () => {
            await server.requestCode(address, locale.value);
            setSentTo(address);
          });
        }}
      >
        <Field label={t("email")} help={t("emailHelp")} error={action.error}>
          <input
            type="text"
            inputMode="email"
            autocomplete="username"
            autocapitalize="none"
            spellcheck={false}
            value={email}
            onInput={(event) => setEmail(event.currentTarget.value)}
            required
          />
        </Field>
        <Button kind="filled" type="submit" busy={action.busy}>
          {t("codeGet")}
        </Button>
      </form>
    </AuthCard>
  );
}

export function SetupView({ token }: { token: string }) {
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const action = useAction();
  return (
    <AuthCard title={t("setupTitle")} lead={t("setupLead")}>
      <form
        class="form"
        onSubmit={(event) => {
          event.preventDefault();
          void action.run(async () => {
            await afterSignIn(await server.setup(token, email.trim(), name, locale.value));
            navigate("#/tree");
          });
        }}
      >
        <Field label={t("yourName")} help={t("nameHelp")}>
          <input autocomplete="name" value={name} onInput={(event) => setName(event.currentTarget.value)} />
        </Field>
        <Field label={t("email")} help={t("setupEmailHelp")} error={action.error}>
          <input
            type="email"
            autocomplete="email"
            autocapitalize="none"
            value={email}
            onInput={(event) => setEmail(event.currentTarget.value)}
            required
          />
        </Field>
        <Button kind="filled" type="submit" busy={action.busy}>
          {t("createAccount")}
        </Button>
      </form>
    </AuthCard>
  );
}

export function SetupNeededView() {
  return <AuthCard title={t("appName")} lead={t("setupNeeded")} />;
}
