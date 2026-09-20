import type { ReactNode } from "react";
import type { GameServer } from "../../lib/types";
import { useT } from "../../lib/i18n";
import { Button } from "../ui/Button";
import { Card } from "../ui/Card";

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-4 px-4 py-2.5">
      <span className="font-sans text-xs uppercase tracking-wide text-text-tertiary">{label}</span>
      <span className="min-w-0 break-all text-right font-sans text-sm text-text-primary">{children}</span>
    </div>
  );
}

export function ServerSettings({
  org,
  server,
  canDelete,
  deleting,
  onDelete,
}: {
  org: string;
  server: GameServer;
  canDelete: boolean;
  deleting: boolean;
  onDelete: () => void;
}) {
  const t = useT();
  const { spec, status, metadata } = server;
  const variables = spec.variables ?? [];

  return (
    <div className="flex max-w-2xl flex-col gap-5">
      <Card className="divide-y divide-border">
        <Row label={t("server.org")}>{org}</Row>
        <Row label="egg">
          {spec.eggRef.name}
          {spec.eggRef.scope ? ` (${spec.eggRef.scope === "Catalog" ? t("server.scopeCatalog") : t("server.scopePrivate")})` : ""}
        </Row>
        <Row label="storage">{spec.storage.size}</Row>
        <Row label={t("server.desiredState")}>{spec.state}</Row>
        <Row label="pod">{status?.podName ?? "—"}</Row>
      </Card>

      <div>
        <div className="mb-2 font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("server.variables")}</div>
        <Card className="divide-y divide-border">
          {variables.length === 0 && <div className="px-4 py-3 font-prose text-sm text-text-tertiary">{t("server.noVariables")}</div>}
          {variables.map((v) => (
            <Row key={v.name} label={v.name}>
              {v.value}
            </Row>
          ))}
        </Card>
      </div>

      {canDelete && (
        <div>
          <div className="mb-2 font-sans text-xs uppercase tracking-wide text-text-tertiary">{t("server.dangerZone")}</div>
          <Button
            variant="danger"
            disabled={deleting}
            onClick={() => {
              if (confirm(t("server.deleteConfirm", { name: metadata.name }))) onDelete();
            }}
          >
            {t("server.deleteServer")}
          </Button>
        </div>
      )}
    </div>
  );
}
