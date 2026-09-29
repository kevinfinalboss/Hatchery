import { useEffect, useState, type FormEvent } from "react";
import QRCode from "qrcode";
import { Button } from "../ui/Button";
import { Card } from "../ui/Card";
import { Field, Input } from "../ui/Input";
import { Modal } from "../ui/Modal";
import { api } from "../../lib/api";
import { useAuth } from "../../lib/auth";
import { errorMessage } from "../../lib/errors";
import { useI18n, useT } from "../../lib/i18n";
import type { TwoFactorSetup, User } from "../../lib/types";

const LOW_CODES = 3;

function RecoveryCodesView({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const t = useT();
  const [saved, setSaved] = useState(false);
  const [copied, setCopied] = useState(false);
  const text = codes.join("\n");

  function download() {
    const url = URL.createObjectURL(new Blob([text + "\n"], { type: "text/plain" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = "hatchery-recovery-codes.txt";
    a.click();
    URL.revokeObjectURL(url);
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="font-prose text-sm text-text-secondary">{t("twoFactor.codesHelp")}</div>
      <div className="grid grid-cols-2 gap-2 border border-border bg-canvas p-4 font-mono text-sm text-text-primary">
        {codes.map((c) => (
          <span key={c}>{c}</span>
        ))}
      </div>
      <div className="flex flex-wrap gap-2">
        <Button
          variant="secondary"
          onClick={() => {
            void navigator.clipboard?.writeText(text).then(() => setCopied(true));
          }}
        >
          {copied ? t("twoFactor.copied") : t("twoFactor.copy")}
        </Button>
        <Button variant="secondary" onClick={download}>
          {t("twoFactor.download")}
        </Button>
      </div>
      <label className="flex items-center gap-2 font-prose text-sm text-text-primary">
        <input type="checkbox" checked={saved} onChange={(e) => setSaved(e.target.checked)} />
        {t("twoFactor.savedThem")}
      </label>
      <div>
        <Button disabled={!saved} onClick={onDone}>
          {t("twoFactor.done")}
        </Button>
      </div>
    </div>
  );
}

function EnableModal({ onClose }: { onClose: () => void }) {
  const t = useT();
  const { setUser } = useAuth();
  const [setup, setSetup] = useState<(TwoFactorSetup & { qr: string }) | null>(null);
  const [code, setCode] = useState("");
  const [codes, setCodes] = useState<string[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [setupError, setSetupError] = useState<unknown>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    api
      .twoFactorSetup()
      .then(async (s) => {
        const qr = await QRCode.toDataURL(s.otpauthUrl, { margin: 1, width: 200 });
        if (!cancelled) setSetup({ ...s, qr });
      })
      .catch((err) => {
        if (!cancelled) setSetupError(err ?? new Error());
      });
    return () => {
      cancelled = true;
    };
  }, []);

  async function confirm(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      setCodes((await api.twoFactorEnable(code)).recoveryCodes);
    } catch (err) {
      setError(errorMessage(err, t("twoFactor.failed")));
    } finally {
      setBusy(false);
    }
  }

  async function finish() {
    setUser(await api.me());
    onClose();
  }

  return (
    <Modal title={t("twoFactor.enableTitle")} onClose={codes ? () => undefined : onClose}>
      {codes ? (
        <RecoveryCodesView codes={codes} onDone={() => void finish()} />
      ) : (
        <form onSubmit={(e) => void confirm(e)} className="flex flex-col gap-4">
          <div className="font-prose text-sm text-text-secondary">{t("twoFactor.scanHelp")}</div>
          {setup && (
            <div className="flex flex-col items-center gap-3">
              <img src={setup.qr} alt={t("twoFactor.qrAlt")} width={200} height={200} className="bg-white p-2" />
              <div className="text-center font-sans text-xs text-text-tertiary">{t("twoFactor.manualEntry")}</div>
              <code className="break-all font-mono text-sm text-text-primary">{setup.secret}</code>
            </div>
          )}
          <Field label={t("twoFactor.code")} htmlFor="tfa-enable-code">
            <Input
              id="tfa-enable-code"
              autoComplete="one-time-code"
              inputMode="numeric"
              maxLength={6}
              placeholder="123456"
              value={code}
              onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))}
              required
            />
          </Field>
          {setupError != null && (
            <div className="font-sans text-sm text-status-failed">{errorMessage(setupError, t("twoFactor.failed"))}</div>
          )}
          {error && <div className="font-sans text-sm text-status-failed">{error}</div>}
          <div>
            <Button type="submit" disabled={busy || !setup || code.length !== 6}>
              {t("twoFactor.confirm")}
            </Button>
          </div>
        </form>
      )}
    </Modal>
  );
}

function ConfirmModal({ mode, onClose }: { mode: "disable" | "regenerate"; onClose: () => void }) {
  const t = useT();
  const { setUser } = useAuth();
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [codes, setCodes] = useState<string[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      if (mode === "disable") {
        await api.twoFactorDisable(password, code.trim());
        setUser(await api.me());
        onClose();
      } else {
        setCodes((await api.twoFactorRegenerate(password, code.trim())).recoveryCodes);
      }
    } catch (err) {
      setError(errorMessage(err, t("twoFactor.failed")));
    } finally {
      setBusy(false);
    }
  }

  async function finish() {
    setUser(await api.me());
    onClose();
  }

  const title = mode === "disable" ? t("twoFactor.disableTitle") : t("twoFactor.regenerateTitle");
  return (
    <Modal title={title} onClose={codes ? () => undefined : onClose}>
      {codes ? (
        <RecoveryCodesView codes={codes} onDone={() => void finish()} />
      ) : (
        <form onSubmit={(e) => void submit(e)} className="flex flex-col gap-4">
          <div className="font-prose text-sm text-text-secondary">
            {mode === "disable" ? t("twoFactor.disableHelp") : t("twoFactor.regenerateHelp")}
          </div>
          <Field label={t("login.password")} htmlFor="tfa-password">
            <Input
              id="tfa-password"
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </Field>
          <Field label={t("twoFactor.codeOrRecovery")} htmlFor="tfa-code">
            <Input id="tfa-code" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} required />
          </Field>
          {error && <div className="font-sans text-sm text-status-failed">{error}</div>}
          <div>
            <Button type="submit" variant={mode === "disable" ? "danger" : "primary"} disabled={busy}>
              {mode === "disable" ? t("twoFactor.disable") : t("twoFactor.regenerate")}
            </Button>
          </div>
        </form>
      )}
    </Modal>
  );
}

export function TwoFactorSection({ user }: { user: User }) {
  const t = useT();
  const { plural } = useI18n();
  const [modal, setModal] = useState<"enable" | "disable" | "regenerate" | null>(null);
  const left = user.twoFactor?.recoveryCodesLeft;
  const enabled = !!user.twoFactor?.enabled;

  return (
    <Card className="flex flex-col gap-3 p-5">
      <div className="font-display text-base font-semibold text-text-primary">{t("twoFactor.title")}</div>
      <div className="font-prose text-sm text-text-secondary">{t("twoFactor.help")}</div>
      {enabled ? (
        <>
          <div className="font-sans text-sm text-text-primary">
            {t("twoFactor.on")}
            {left !== undefined && <> · {plural("twoFactor.codesLeft", left)}</>}
          </div>
          {left !== undefined && left <= LOW_CODES && (
            <div className="font-sans text-sm text-status-failed">{t("twoFactor.lowCodes")}</div>
          )}
          <div className="flex flex-wrap gap-2">
            <Button variant="secondary" onClick={() => setModal("regenerate")}>
              {t("twoFactor.regenerate")}
            </Button>
            <Button variant="danger" onClick={() => setModal("disable")}>
              {t("twoFactor.disable")}
            </Button>
          </div>
        </>
      ) : (
        <div>
          <Button onClick={() => setModal("enable")}>{t("twoFactor.enable")}</Button>
        </div>
      )}
      {modal === "enable" && <EnableModal onClose={() => setModal(null)} />}
      {(modal === "disable" || modal === "regenerate") && <ConfirmModal mode={modal} onClose={() => setModal(null)} />}
    </Card>
  );
}
