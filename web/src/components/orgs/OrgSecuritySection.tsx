import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "../ui/Button";
import { Card } from "../ui/Card";
import { api } from "../../lib/api";
import { useAuth } from "../../lib/auth";
import { errorMessage } from "../../lib/errors";
import { useI18n, useT } from "../../lib/i18n";

export function OrgSecurityCard({ org }: { org: string }) {
  const t = useT();
  const { plural } = useI18n();
  const { user } = useAuth();
  const qc = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const { data } = useQuery({ queryKey: ["org-security", org], queryFn: () => api.orgSecurity(org), enabled: !!org });
  const hasOwn2FA = !!user?.twoFactor?.enabled;

  async function set(on: boolean) {
    setError(null);
    setBusy(true);
    try {
      await api.setOrgSecurity(org, on);
      setConfirming(false);
      await qc.invalidateQueries({ queryKey: ["org-security", org] });
      await qc.invalidateQueries({ queryKey: ["orgs"] });
      await qc.invalidateQueries({ queryKey: ["members", org] });
    } catch (err) {
      setError(errorMessage(err, t("twoFactor.failed")));
    } finally {
      setBusy(false);
    }
  }

  if (!data) return null;
  const without = data.membersWithout2fa ?? 0;
  return (
    <Card className="flex flex-col gap-3 p-5">
      <div className="font-display text-base font-semibold text-text-primary">{t("twoFactor.orgTitle")}</div>
      <div className="font-prose text-sm text-text-secondary">{t("twoFactor.orgHelp")}</div>
      <div className="font-sans text-sm text-text-primary">
        {data.require2fa ? t("twoFactor.orgRequired") : t("twoFactor.orgNotRequired")}
        {data.membersWithout2fa !== undefined && <> · {plural("twoFactor.membersWithout", without)}</>}
      </div>
      {!hasOwn2FA && <div className="font-sans text-sm text-text-tertiary">{t("twoFactor.needOwn")}</div>}
      {confirming ? (
        <div className="flex flex-col gap-3 border border-border p-3">
          <div className="font-prose text-sm text-text-primary">
            {without > 0 ? plural("twoFactor.orgConfirmBlocked", without) : t("twoFactor.orgConfirmNone")}
          </div>
          <div className="flex flex-wrap gap-2">
            <Button disabled={busy} onClick={() => void set(true)}>
              {t("twoFactor.orgRequire")}
            </Button>
            <Button variant="secondary" onClick={() => setConfirming(false)}>
              {t("common.cancel")}
            </Button>
          </div>
        </div>
      ) : (
        <div>
          {data.require2fa ? (
            <Button variant="secondary" disabled={busy || !hasOwn2FA} onClick={() => void set(false)}>
              {t("twoFactor.orgStopRequiring")}
            </Button>
          ) : (
            <Button disabled={!hasOwn2FA} onClick={() => setConfirming(true)}>
              {t("twoFactor.orgRequire")}
            </Button>
          )}
        </div>
      )}
      {error && <div className="font-sans text-sm text-status-failed">{error}</div>}
    </Card>
  );
}

export function TwoFactorGate({ orgName }: { orgName: string }) {
  const t = useT();
  return (
    <Card className="flex max-w-xl flex-col gap-3 p-6">
      <div className="font-display text-lg font-bold text-text-primary">🔒 {t("twoFactor.gateTitle")}</div>
      <div className="font-prose text-sm text-text-secondary">{t("twoFactor.gateHelp", { org: orgName })}</div>
      <div>
        <Link to="/account">
          <Button>{t("twoFactor.gateAction")}</Button>
        </Link>
      </div>
    </Card>
  );
}

export function PlatformSecurityCard() {
  const t = useT();
  const { user } = useAuth();
  const qc = useQueryClient();
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const { data } = useQuery({ queryKey: ["platform-security"], queryFn: api.platformSecurity });
  const hasOwn2FA = !!user?.twoFactor?.enabled;

  async function set(on: boolean) {
    setError(null);
    setBusy(true);
    try {
      await api.setPlatformSecurity(on);
      await qc.invalidateQueries({ queryKey: ["platform-security"] });
    } catch (err) {
      setError(errorMessage(err, t("twoFactor.failed")));
    } finally {
      setBusy(false);
    }
  }

  if (!data) return null;
  return (
    <Card className="flex flex-col gap-3 p-5">
      <label className="flex items-center gap-2 font-prose text-sm text-text-primary">
        <input
          type="checkbox"
          checked={data.requireAdminTwoFactor}
          disabled={busy || !hasOwn2FA}
          onChange={(e) => void set(e.target.checked)}
        />
        {t("twoFactor.platformRequire")}
      </label>
      <div className="font-prose text-sm text-text-secondary">{t("twoFactor.platformHelp")}</div>
      {!hasOwn2FA && <div className="font-sans text-sm text-text-tertiary">{t("twoFactor.needOwn")}</div>}
      {error && <div className="font-sans text-sm text-status-failed">{error}</div>}
    </Card>
  );
}
