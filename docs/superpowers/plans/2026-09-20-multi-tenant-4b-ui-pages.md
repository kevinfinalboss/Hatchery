# Multi-tenant 4b — UI: páginas de Eggs, Membros, Auditoria e Organizações — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Entregar as quatro telas novas do multi-tenant e limpar a tela de Usuários dos grants antigos: Eggs (catálogo + privados), Membros da org, Auditoria e Organizações (administração da plataforma).

**Architecture:** Cada página é um arquivo em `web/src/pages/`, consome o `api` e o `useOrg` da parte 4a, e segue o padrão visual existente (`Card`, `Button`, `Field`/`Input`). A UI esconde o que o papel não permite, espelhando as regras do servidor, mas **o servidor continua sendo a autoridade** — todo erro de permissão que passar é exibido como mensagem.

**Tech Stack:** React 19, TypeScript, TanStack Query v5 (`useQuery`, `useMutation`, `useInfiniteQuery`), React Router 7, Tailwind v4.

**Spec:** `docs/superpowers/specs/2026-09-20-multi-tenant-design.md`. Depende do plano **4a** (API client, `OrgProvider`, rotas) e dos planos 2b/2c/3 (endpoints).

## Regras de execução deste projeto

- **NÃO fazer `git add`/`git commit` de código.** O usuário commita. Sem passos de commit.
- **Pedir aprovação explícita ao usuário antes de começar a executar a Task 1.**
- Comunicação com o usuário em português; **textos da UI em português**.
- **Não validar no navegador por conta própria** (convenção do projeto). Verificação automática: `npx tsc --noEmit` e `npm run build`. A lista de validação manual está na Task 6 para o usuário executar.

## Global Constraints

- Regras espelhadas do servidor (só para esconder botões, nunca como segurança):
  - `owner` > `admin` > `member`; só `owner` mexe em `owner`; qualquer membro pode sair sozinho; o último owner não sai (409 do servidor é mostrado tal como vem).
  - Egg privado: `admin`+ escreve. Catálogo: só platform admin (`user.isAdmin`).
  - Auditoria da org: `admin`+. Organizações/quota/usuários: platform admin.
- Erros de mutation sempre visíveis (uma `<div className="font-sans text-sm text-status-failed">`), nunca silenciosos (lição do M7: mutation sem `onError` engole falha).
- Confirmação (`confirm(...)`) antes de qualquer exclusão.
- Todos os arquivos em `web/src/`.

## File Structure

- Create `pages/EggsPage.tsx`, `pages/MembersPage.tsx`, `pages/AuditPage.tsx`, `pages/OrgsPage.tsx`
- Modify `pages/UsersPage.tsx` (remover permissões por servidor)
- Create `lib/errors.ts` (helper `errorMessage`)

---

### Task 1: Helper de erro e limpeza da tela de Usuários

**Files:**
- Create: `web/src/lib/errors.ts`
- Modify: `web/src/pages/UsersPage.tsx`

**Interfaces:**
- Produces: `errorMessage(err: unknown, fallback: string): string`.

- [ ] **Step 1: Criar o helper**

Create `web/src/lib/errors.ts`:

```ts
/** Message to show for a failed request: the server's own text when there is one. */
export function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error && err.message ? err.message : fallback;
}
```

- [ ] **Step 2: Limpar `UsersPage`**

In `web/src/pages/UsersPage.tsx`:
1. **Apagar** o componente `UserPermissions` (do início do arquivo, logo após os imports, até o fechamento antes de `function CreateUserForm`) e a linha `<UserPermissions user={user} />` dentro do `Card` de cada usuário.
2. Remover imports que ficarem sem uso (`GameServerRef`, `User` se só era usado ali, etc.). O `Card` do usuário passa a conter só o cabeçalho (nome, badge Admin, botão Excluir); trocar `className="flex flex-col gap-4 p-5"` por `className="flex items-center justify-between p-5"` e remover o `div` wrapper extra se sobrar (o cabeçalho vira o conteúdo direto do `Card`).
3. Mostrar o erro de exclusão (o servidor devolve 409 quando o usuário é o único owner de uma org). Substituir o `deleteUser` por:

```tsx
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const deleteUser = useMutation({
    mutationFn: (id: number) => api.deleteUser(id),
    onSuccess: () => {
      setDeleteError(null);
      void queryClient.invalidateQueries({ queryKey: ["users"] });
    },
    onError: (err) => setDeleteError(errorMessage(err, "Falha ao excluir usuário")),
  });
```
e, logo abaixo do `<CreateUserForm />`, `{deleteError && <div className="font-sans text-sm text-status-failed">{deleteError}</div>}`. Importar `errorMessage` de `../lib/errors` e `useState` de `react` se ainda não estiverem.

- [ ] **Step 3: Typecheck**

Run: `cd /home/kevingomes/Hatchery/web && npx tsc --noEmit 2>&1 | head -20`
Expected: só erros das páginas que ainda não existem (Tasks 2–5), se as rotas do 4a estiverem apontando para elas; `UsersPage.tsx` sem erros.

---

### Task 2: Página de Membros

**Files:**
- Create: `web/src/pages/MembersPage.tsx`

**Interfaces:**
- Consumes: `api.listMembers|addMember|setMemberRole|removeMember`, `useAuth` (id do usuário logado, para "sair"), `useOrg`, `atLeast`.

- [ ] **Step 1: Escrever a página**

Create `web/src/pages/MembersPage.tsx`:

```tsx
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";
import { atLeast, useOrg } from "../lib/org";
import { errorMessage } from "../lib/errors";
import type { Member, OrgRole } from "../lib/types";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { Field, Input } from "../components/ui/Input";

const roleLabel: Record<OrgRole, string> = { owner: "Owner", admin: "Admin", member: "Membro" };

const selectClasses =
  "rounded-lg border border-border-strong bg-surface px-3 py-2 font-sans text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-primary";

function AddMemberForm({ org, callerRole }: { org: string; callerRole: OrgRole }) {
  const queryClient = useQueryClient();
  const [username, setUsername] = useState("");
  const [role, setRole] = useState<OrgRole>("member");
  const [error, setError] = useState<string | null>(null);

  const add = useMutation({
    mutationFn: () => api.addMember(org, username.trim(), role),
    onSuccess: () => {
      setUsername("");
      setRole("member");
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ["members", org] });
    },
    onError: (err) => setError(errorMessage(err, "Falha ao adicionar membro")),
  });

  return (
    <Card className="flex flex-col gap-4 p-5">
      <div className="font-display text-base font-semibold text-text-primary">Adicionar membro</div>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          setError(null);
          add.mutate();
        }}
        className="flex flex-wrap items-end gap-4"
      >
        <Field label="Usuário" htmlFor="member-username">
          <Input id="member-username" value={username} onChange={(e) => setUsername(e.target.value)} required />
        </Field>
        <Field label="Papel" htmlFor="member-role">
          <select id="member-role" value={role} onChange={(e) => setRole(e.target.value as OrgRole)} className={selectClasses}>
            <option value="member">Membro</option>
            <option value="admin">Admin</option>
            {callerRole === "owner" && <option value="owner">Owner</option>}
          </select>
        </Field>
        <Button type="submit" disabled={add.isPending}>
          {add.isPending ? "Adicionando…" : "Adicionar"}
        </Button>
      </form>
      {error && <div className="font-sans text-sm text-status-failed">{error}</div>}
    </Card>
  );
}

function MemberRow({
  org,
  member,
  callerRole,
  selfId,
  onError,
}: {
  org: string;
  member: Member;
  callerRole: OrgRole;
  selfId: number | undefined;
  onError: (msg: string | null) => void;
}) {
  const queryClient = useQueryClient();
  const isSelf = member.userId === selfId;
  // Only an owner deals in owners; an admin manages members and other admins.
  const canManage = atLeast(callerRole, "admin") && (member.role !== "owner" || callerRole === "owner");
  const canLeaveOnly = isSelf && !canManage;

  const invalidate = () => void queryClient.invalidateQueries({ queryKey: ["members", org] });

  const changeRole = useMutation({
    mutationFn: (role: OrgRole) => api.setMemberRole(org, member.userId, role),
    onSuccess: () => {
      onError(null);
      invalidate();
    },
    onError: (err) => onError(errorMessage(err, "Falha ao alterar o papel")),
  });
  const remove = useMutation({
    mutationFn: () => api.removeMember(org, member.userId),
    onSuccess: () => {
      onError(null);
      invalidate();
      // Leaving the org removes it from the user's own list too.
      if (isSelf) void queryClient.invalidateQueries({ queryKey: ["orgs"] });
    },
    onError: (err) => onError(errorMessage(err, "Falha ao remover membro")),
  });

  return (
    <Card className="flex items-center justify-between gap-4 p-4">
      <div className="min-w-0">
        <span className="font-display text-[15px] font-semibold text-text-primary">{member.username}</span>
        {isSelf && <span className="ml-2 font-sans text-xs text-text-tertiary">(você)</span>}
      </div>
      <div className="flex items-center gap-2">
        {canManage ? (
          <select
            aria-label={`Papel de ${member.username}`}
            value={member.role}
            disabled={changeRole.isPending}
            onChange={(e) => changeRole.mutate(e.target.value as OrgRole)}
            className={selectClasses}
          >
            <option value="member">Membro</option>
            <option value="admin">Admin</option>
            {(callerRole === "owner" || member.role === "owner") && <option value="owner">Owner</option>}
          </select>
        ) : (
          <span className="rounded-full bg-primary/15 px-2 py-0.5 font-sans text-[11px] font-semibold text-primary">
            {roleLabel[member.role]}
          </span>
        )}
        {(canManage || canLeaveOnly) && (
          <Button
            variant="ghost"
            disabled={remove.isPending}
            onClick={() => {
              const q = isSelf ? "Sair desta organização?" : `Remover ${member.username} da organização?`;
              if (confirm(q)) remove.mutate();
            }}
          >
            {isSelf ? "Sair" : "Remover"}
          </Button>
        )}
      </div>
    </Card>
  );
}

export function MembersPage() {
  const { user } = useAuth();
  const { current } = useOrg();
  const [rowError, setRowError] = useState<string | null>(null);
  const org = current?.slug ?? "";

  const { data: members, isLoading, error } = useQuery({
    queryKey: ["members", org],
    queryFn: () => api.listMembers(org),
    enabled: !!org,
  });

  if (!current) return <div className="font-sans text-sm text-text-secondary">Selecione uma organização.</div>;

  return (
    <div className="flex flex-col gap-6">
      <div>
        <div className="font-display text-2xl font-bold text-text-primary">Membros</div>
        <div className="mt-0.5 font-sans text-sm text-text-secondary">{current.name}</div>
      </div>

      {atLeast(current.role, "admin") && <AddMemberForm org={org} callerRole={current.role} />}

      {isLoading && <div className="font-sans text-sm text-text-secondary">Carregando…</div>}
      {error && <div className="font-sans text-sm text-status-failed">{errorMessage(error, "Falha ao carregar membros")}</div>}
      {rowError && <div className="font-sans text-sm text-status-failed">{rowError}</div>}

      <div className="flex flex-col gap-3">
        {(members ?? []).map((m) => (
          <MemberRow key={m.userId} org={org} member={m} callerRole={current.role} selfId={user?.id} onError={setRowError} />
        ))}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd /home/kevingomes/Hatchery/web && npx tsc --noEmit 2>&1 | grep MembersPage | head`
Expected: sem erros.

---

### Task 3: Página de Eggs

**Files:**
- Create: `web/src/pages/EggsPage.tsx`

**Interfaces:**
- Consumes: `api.listOrgEggs|createOrgEgg|updateOrgEgg|deleteOrgEgg|createCatalogEgg|updateCatalogEgg|deleteCatalogEgg`, `useAuth().user.isAdmin`, `useOrg`, `atLeast`.

Comportamento: lista Eggs do catálogo (badge "Catálogo") e privados (badge "Privado") da org atual. `admin`+ cria/edita/exclui privados; platform admin também cria/edita/exclui os do catálogo. Editar = textarea com o **spec em JSON** (o Egg é código, quem edita é admin; um gerador de formulário completo é YAGNI).

- [ ] **Step 1: Escrever a página**

Create `web/src/pages/EggsPage.tsx`:

```tsx
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";
import { atLeast, useOrg } from "../lib/org";
import { errorMessage } from "../lib/errors";
import type { EggEntry, EggSpec } from "../lib/types";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { Field, Input } from "../components/ui/Input";

const TEMPLATE: EggSpec = {
  image: "",
  startCommand: "",
  variables: [],
  ports: [{ name: "game", containerPort: 25565, protocol: "TCP", default: true }],
};

function EggEditor({
  initialName,
  initialSpec,
  lockName,
  saving,
  error,
  onSave,
  onCancel,
}: {
  initialName: string;
  initialSpec: EggSpec;
  lockName: boolean;
  saving: boolean;
  error: string | null;
  onSave: (name: string, spec: EggSpec) => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(initialName);
  const [json, setJson] = useState(JSON.stringify(initialSpec, null, 2));
  const [parseError, setParseError] = useState<string | null>(null);

  function submit() {
    try {
      const spec = JSON.parse(json) as EggSpec;
      setParseError(null);
      onSave(name.trim(), spec);
    } catch (e) {
      setParseError(`JSON inválido: ${e instanceof Error ? e.message : String(e)}`);
    }
  }

  return (
    <Card className="flex flex-col gap-4 p-5">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
        className="flex flex-col gap-4"
      >
        <Field label="Nome" htmlFor="egg-name">
          <Input id="egg-name" value={name} onChange={(e) => setName(e.target.value)} disabled={lockName} required />
        </Field>
        <Field label="Spec (JSON)" htmlFor="egg-spec">
          <textarea
            id="egg-spec"
            value={json}
            onChange={(e) => setJson(e.target.value)}
            spellCheck={false}
            rows={14}
            className="w-full rounded-lg border border-border-strong bg-surface px-3 py-2 font-mono text-xs text-text-primary focus:outline-none focus:ring-2 focus:ring-primary"
          />
        </Field>
        <div className="font-sans text-xs text-text-tertiary">
          O Egg define a imagem e os scripts que rodam no cluster. Roda isolado no namespace da organização, mas confie
          apenas em imagens e scripts que você conhece.
        </div>
        <div className="flex gap-2">
          <Button type="submit" disabled={saving}>
            {saving ? "Salvando…" : "Salvar"}
          </Button>
          <Button type="button" variant="ghost" onClick={onCancel}>
            Cancelar
          </Button>
        </div>
      </form>
      {(parseError || error) && <div className="font-sans text-sm text-status-failed">{parseError ?? error}</div>}
    </Card>
  );
}

export function EggsPage() {
  const { user } = useAuth();
  const { current } = useOrg();
  const queryClient = useQueryClient();
  const org = current?.slug ?? "";
  const canWritePrivate = atLeast(current?.role, "admin");
  const canWriteCatalog = !!user?.isAdmin;

  // editing: null = closed, "new-private" / "new-catalog" = create, or the entry being edited.
  const [editing, setEditing] = useState<null | "new-private" | "new-catalog" | EggEntry>(null);
  const [error, setError] = useState<string | null>(null);

  const { data: eggs, isLoading } = useQuery({
    queryKey: ["eggs", org],
    queryFn: () => api.listOrgEggs(org),
    enabled: !!org,
  });

  const refresh = () => void queryClient.invalidateQueries({ queryKey: ["eggs", org] });
  const done = () => {
    setEditing(null);
    setError(null);
    refresh();
  };
  const fail = (err: unknown) => setError(errorMessage(err, "Falha ao salvar o Egg"));

  const save = useMutation({
    mutationFn: async ({ name, spec }: { name: string; spec: EggSpec }) => {
      if (editing === "new-private") return api.createOrgEgg(org, name, spec);
      if (editing === "new-catalog") return api.createCatalogEgg(name, spec);
      if (editing && editing.scope === "Catalog") return api.updateCatalogEgg(editing.name, spec);
      if (editing) return api.updateOrgEgg(org, editing.name, spec);
    },
    onSuccess: done,
    onError: fail,
  });

  const remove = useMutation({
    mutationFn: (egg: EggEntry) => (egg.scope === "Catalog" ? api.deleteCatalogEgg(egg.name) : api.deleteOrgEgg(org, egg.name)),
    onSuccess: done,
    onError: (err) => setError(errorMessage(err, "Falha ao excluir o Egg")),
  });

  if (!current) return <div className="font-sans text-sm text-text-secondary">Selecione uma organização.</div>;

  const canWrite = (egg: EggEntry) => (egg.scope === "Catalog" ? canWriteCatalog : canWritePrivate);

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <div className="font-display text-2xl font-bold text-text-primary">Eggs</div>
          <div className="mt-0.5 font-sans text-sm text-text-secondary">
            Catálogo da plataforma e Eggs privados de {current.name}
          </div>
        </div>
        {editing === null && (
          <div className="flex gap-2">
            {canWritePrivate && <Button onClick={() => setEditing("new-private")}>+ Egg privado</Button>}
            {canWriteCatalog && (
              <Button variant="secondary" onClick={() => setEditing("new-catalog")}>
                + Egg de catálogo
              </Button>
            )}
          </div>
        )}
      </div>

      {editing !== null && (
        <EggEditor
          key={typeof editing === "string" ? editing : `${editing.scope}:${editing.name}`}
          initialName={typeof editing === "string" ? "" : editing.name}
          initialSpec={typeof editing === "string" ? TEMPLATE : editing.spec}
          lockName={typeof editing !== "string"}
          saving={save.isPending}
          error={error}
          onSave={(name, spec) => {
            setError(null);
            save.mutate({ name, spec });
          }}
          onCancel={() => {
            setEditing(null);
            setError(null);
          }}
        />
      )}

      {editing === null && error && <div className="font-sans text-sm text-status-failed">{error}</div>}
      {isLoading && <div className="font-sans text-sm text-text-secondary">Carregando…</div>}

      <div className="flex flex-col gap-3">
        {(eggs ?? []).map((egg) => (
          <Card key={`${egg.scope}:${egg.name}`} className="flex items-center justify-between gap-4 p-4">
            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <span className="font-display text-[15px] font-semibold text-text-primary">{egg.name}</span>
                <span className="rounded-full bg-primary/15 px-2 py-0.5 font-sans text-[11px] font-semibold text-primary">
                  {egg.scope === "Catalog" ? "Catálogo" : "Privado"}
                </span>
              </div>
              <div className="truncate font-mono text-xs text-text-tertiary">{egg.spec.image}</div>
            </div>
            {canWrite(egg) && (
              <div className="flex gap-2">
                <Button variant="secondary" onClick={() => setEditing(egg)}>
                  Editar
                </Button>
                <Button
                  variant="ghost"
                  disabled={remove.isPending}
                  onClick={() => {
                    if (confirm(`Excluir o Egg ${egg.name}?`)) remove.mutate(egg);
                  }}
                >
                  Excluir
                </Button>
              </div>
            )}
          </Card>
        ))}
        {!isLoading && (eggs ?? []).length === 0 && (
          <div className="font-sans text-sm text-text-tertiary">Nenhum Egg disponível ainda.</div>
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd /home/kevingomes/Hatchery/web && npx tsc --noEmit 2>&1 | grep EggsPage | head`
Expected: sem erros.

---

### Task 4: Página de Auditoria

**Files:**
- Create: `web/src/pages/AuditPage.tsx`

**Interfaces:**
- Consumes: `api.listOrgAudit(org, before?)`, `api.listPlatformAudit(before?)`, `useInfiniteQuery` (TanStack v5: `initialPageParam` obrigatório).

Comportamento: tabela dos eventos da org atual (a rota já é protegida por `RequireOrgAdmin`), "Carregar mais" usa `nextBefore`. Platform admin vê um seletor "Org / Plataforma" para alternar para os eventos de plataforma (logins).

- [ ] **Step 1: Escrever a página**

Create `web/src/pages/AuditPage.tsx`:

```tsx
import { useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";
import { useOrg } from "../lib/org";
import { errorMessage } from "../lib/errors";
import type { AuditEvent } from "../lib/types";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";

const actionLabel: Record<string, string> = {
  "gameserver.create": "Criou servidor",
  "gameserver.delete": "Excluiu servidor",
  "gameserver.state": "Iniciou/parou servidor",
  "gameserver.sftp-session": "Abriu sessão SFTP",
  "gameserver.console-ticket": "Abriu o console",
  "file.write": "Editou arquivo",
  "file.mkdir": "Criou pasta",
  "file.rename": "Renomeou arquivo",
  "file.delete": "Excluiu arquivos",
  "file.copy": "Copiou arquivo",
  "file.upload": "Enviou arquivo",
  "file.compress": "Compactou arquivos",
  "file.decompress": "Descompactou arquivo",
  "member.add": "Adicionou membro",
  "member.role": "Alterou papel",
  "member.remove": "Removeu membro",
  "egg.create": "Criou Egg",
  "egg.update": "Editou Egg",
  "egg.delete": "Excluiu Egg",
  "catalog.egg.create": "Criou Egg de catálogo",
  "catalog.egg.update": "Editou Egg de catálogo",
  "catalog.egg.delete": "Excluiu Egg de catálogo",
  "org.create": "Criou organização",
  "org.delete": "Excluiu organização",
  "org.quota": "Alterou quota",
  "login.success": "Login",
  "login.failure": "Login falhou",
  "login.blocked": "Login bloqueado (rate limit)",
};

const outcomeClasses: Record<AuditEvent["outcome"], string> = {
  success: "text-status-running",
  denied: "text-status-failed",
  failed: "text-status-failed",
};

function EventRow({ e }: { e: AuditEvent }) {
  return (
    <tr className="border-t border-border">
      <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs text-text-tertiary">
        {new Date(e.createdAt).toLocaleString("pt-BR")}
      </td>
      <td className="px-4 py-2.5 font-sans text-sm text-text-primary">{e.actorUsername}</td>
      <td className="px-4 py-2.5 font-sans text-sm text-text-primary">{actionLabel[e.action] ?? e.action}</td>
      <td className="px-4 py-2.5 font-mono text-xs text-text-secondary">
        {e.targetName ? `${e.targetType ?? ""} ${e.targetName}`.trim() : "—"}
      </td>
      <td className={`px-4 py-2.5 font-sans text-xs font-semibold ${outcomeClasses[e.outcome]}`}>{e.outcome}</td>
      <td className="px-4 py-2.5 font-mono text-xs text-text-tertiary">{e.ip ?? ""}</td>
    </tr>
  );
}

export function AuditPage() {
  const { user } = useAuth();
  const { current } = useOrg();
  const [scope, setScope] = useState<"org" | "platform">("org");
  const org = current?.slug ?? "";
  const platform = scope === "platform" && !!user?.isAdmin;

  const { data, isLoading, error, fetchNextPage, hasNextPage, isFetchingNextPage } = useInfiniteQuery({
    queryKey: ["audit", platform ? "platform" : org],
    queryFn: ({ pageParam }) => (platform ? api.listPlatformAudit(pageParam) : api.listOrgAudit(org, pageParam)),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (last) => last.nextBefore ?? undefined,
    enabled: platform || !!org,
  });

  const events = data?.pages.flatMap((p) => p.events) ?? [];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <div className="font-display text-2xl font-bold text-text-primary">Auditoria</div>
          <div className="mt-0.5 font-sans text-sm text-text-secondary">
            {platform ? "Eventos da plataforma (logins)" : `Ações em ${current?.name ?? ""}`}
          </div>
        </div>
        {user?.isAdmin && (
          <select
            aria-label="Escopo da auditoria"
            value={scope}
            onChange={(e) => setScope(e.target.value as "org" | "platform")}
            className="rounded-lg border border-border-strong bg-surface px-3 py-2 font-sans text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-primary"
          >
            <option value="org">Organização atual</option>
            <option value="platform">Plataforma</option>
          </select>
        )}
      </div>

      {error && <div className="font-sans text-sm text-status-failed">{errorMessage(error, "Falha ao carregar a auditoria")}</div>}
      {isLoading && <div className="font-sans text-sm text-text-secondary">Carregando…</div>}

      <Card className="overflow-x-auto">
        <table className="w-full text-left">
          <thead>
            <tr className="font-sans text-xs uppercase tracking-wide text-text-tertiary">
              <th className="px-4 py-2.5 font-medium">Quando</th>
              <th className="px-4 py-2.5 font-medium">Quem</th>
              <th className="px-4 py-2.5 font-medium">Ação</th>
              <th className="px-4 py-2.5 font-medium">Alvo</th>
              <th className="px-4 py-2.5 font-medium">Resultado</th>
              <th className="px-4 py-2.5 font-medium">IP</th>
            </tr>
          </thead>
          <tbody>
            {events.map((e) => (
              <EventRow key={e.id} e={e} />
            ))}
          </tbody>
        </table>
        {!isLoading && events.length === 0 && (
          <div className="px-4 py-6 font-sans text-sm text-text-tertiary">Nenhum evento registrado ainda.</div>
        )}
      </Card>

      {hasNextPage && (
        <div>
          <Button variant="secondary" disabled={isFetchingNextPage} onClick={() => void fetchNextPage()}>
            {isFetchingNextPage ? "Carregando…" : "Carregar mais"}
          </Button>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd /home/kevingomes/Hatchery/web && npx tsc --noEmit 2>&1 | grep AuditPage | head`
Expected: sem erros. (Se `text-status-running` não existir como token do Tailwind, conferir os tokens em `web/src/index.css` e usar o de sucesso que existir, p.ex. o mesmo do `StatusBadge` para `Running`.)

---

### Task 5: Página de Organizações (plataforma)

**Files:**
- Create: `web/src/pages/OrgsPage.tsx`

**Interfaces:**
- Consumes: `api.listOrgs|getOrg|createOrg|updateOrgQuota|deleteOrg`. A rota `/orgs` já é protegida por `RequireAdmin` (4a).

Comportamento: lista todas as orgs (platform admin vê todas), formulário de criar (slug, nome, username do owner, quota), e por org: fase do Tenant, quota editável e exclusão (o servidor responde 409 se ainda houver servidores).

- [ ] **Step 1: Escrever a página**

Create `web/src/pages/OrgsPage.tsx`:

```tsx
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { errorMessage } from "../lib/errors";
import type { OrgQuota, OrgSummary } from "../lib/types";
import { Button } from "../components/ui/Button";
import { Card } from "../components/ui/Card";
import { Field, Input } from "../components/ui/Input";

const DEFAULT_QUOTA: OrgQuota = { cpu: "4", memory: "8Gi", storage: "50Gi", maxGameServers: 3 };

function QuotaFields({ quota, onChange }: { quota: OrgQuota; onChange: (q: OrgQuota) => void }) {
  return (
    <>
      <Field label="CPU" htmlFor="q-cpu">
        <Input id="q-cpu" value={quota.cpu} onChange={(e) => onChange({ ...quota, cpu: e.target.value })} required className="w-24" />
      </Field>
      <Field label="Memória" htmlFor="q-mem">
        <Input id="q-mem" value={quota.memory} onChange={(e) => onChange({ ...quota, memory: e.target.value })} required className="w-28" />
      </Field>
      <Field label="Storage" htmlFor="q-sto">
        <Input id="q-sto" value={quota.storage} onChange={(e) => onChange({ ...quota, storage: e.target.value })} required className="w-28" />
      </Field>
      <Field label="Máx. servidores" htmlFor="q-max">
        <Input
          id="q-max"
          type="number"
          min={0}
          value={quota.maxGameServers}
          onChange={(e) => onChange({ ...quota, maxGameServers: Number(e.target.value) })}
          required
          className="w-28"
        />
      </Field>
    </>
  );
}

function CreateOrgForm({ onClose }: { onClose: () => void }) {
  const queryClient = useQueryClient();
  const [slug, setSlug] = useState("");
  const [name, setName] = useState("");
  const [owner, setOwner] = useState("");
  const [quota, setQuota] = useState<OrgQuota>(DEFAULT_QUOTA);
  const [error, setError] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: () => api.createOrg({ slug: slug.trim(), name: name.trim(), ownerUsername: owner.trim(), quota }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["orgs"] });
      onClose();
    },
    onError: (err) => setError(errorMessage(err, "Falha ao criar a organização")),
  });

  return (
    <Card className="flex flex-col gap-4 p-5">
      <div className="font-display text-base font-semibold text-text-primary">Nova organização</div>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          setError(null);
          create.mutate();
        }}
        className="flex flex-wrap items-end gap-4"
      >
        <Field label="Slug (minúsculas, sem espaços)" htmlFor="org-slug">
          <Input id="org-slug" value={slug} onChange={(e) => setSlug(e.target.value)} pattern="[a-z0-9]([\-a-z0-9]{0,30}[a-z0-9])?" required />
        </Field>
        <Field label="Nome" htmlFor="org-name">
          <Input id="org-name" value={name} onChange={(e) => setName(e.target.value)} required />
        </Field>
        <Field label="Owner (usuário existente)" htmlFor="org-owner">
          <Input id="org-owner" value={owner} onChange={(e) => setOwner(e.target.value)} required />
        </Field>
        <QuotaFields quota={quota} onChange={setQuota} />
        <div className="flex gap-2">
          <Button type="submit" disabled={create.isPending}>
            {create.isPending ? "Criando…" : "Criar"}
          </Button>
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancelar
          </Button>
        </div>
      </form>
      {error && <div className="font-sans text-sm text-status-failed">{error}</div>}
    </Card>
  );
}

function OrgRow({ org }: { org: OrgSummary }) {
  const queryClient = useQueryClient();
  const { data: detail } = useQuery({ queryKey: ["org", org.slug], queryFn: () => api.getOrg(org.slug), refetchInterval: 10000 });
  const [editing, setEditing] = useState(false);
  const [quota, setQuota] = useState<OrgQuota>(DEFAULT_QUOTA);
  const [error, setError] = useState<string | null>(null);

  const saveQuota = useMutation({
    mutationFn: () => api.updateOrgQuota(org.slug, quota),
    onSuccess: () => {
      setEditing(false);
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ["org", org.slug] });
    },
    onError: (err) => setError(errorMessage(err, "Falha ao alterar a quota")),
  });
  const remove = useMutation({
    mutationFn: () => api.deleteOrg(org.slug),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["orgs"] }),
    onError: (err) => setError(errorMessage(err, "Falha ao excluir a organização")),
  });

  return (
    <Card className="flex flex-col gap-3 p-5">
      <div className="flex items-center justify-between gap-4">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="font-display text-[15px] font-semibold text-text-primary">{org.name}</span>
            <span className="font-mono text-xs text-text-tertiary">{org.slug}</span>
            {detail && (
              <span className="rounded-full bg-primary/15 px-2 py-0.5 font-sans text-[11px] font-semibold text-primary">
                {detail.phase}
              </span>
            )}
          </div>
          {detail?.quota && !editing && (
            <div className="mt-1 font-mono text-xs text-text-secondary">
              CPU {detail.quota.cpu} · Memória {detail.quota.memory} · Storage {detail.quota.storage} · até{" "}
              {detail.quota.maxGameServers} servidores
            </div>
          )}
        </div>
        <div className="flex gap-2">
          <Button
            variant="secondary"
            onClick={() => {
              if (detail?.quota) setQuota(detail.quota);
              setEditing((v) => !v);
            }}
            disabled={!detail?.quota}
          >
            Quota
          </Button>
          <Button
            variant="ghost"
            disabled={remove.isPending}
            onClick={() => {
              if (confirm(`Excluir a organização ${org.name}? Ela precisa estar sem servidores.`)) remove.mutate();
            }}
          >
            Excluir
          </Button>
        </div>
      </div>

      {editing && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            setError(null);
            saveQuota.mutate();
          }}
          className="flex flex-wrap items-end gap-4"
        >
          <QuotaFields quota={quota} onChange={setQuota} />
          <Button type="submit" disabled={saveQuota.isPending}>
            {saveQuota.isPending ? "Salvando…" : "Salvar quota"}
          </Button>
        </form>
      )}
      {error && <div className="font-sans text-sm text-status-failed">{error}</div>}
    </Card>
  );
}

export function OrgsPage() {
  const [creating, setCreating] = useState(false);
  const { data: orgs, isLoading, error } = useQuery({ queryKey: ["orgs"], queryFn: api.listOrgs });

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <div>
          <div className="font-display text-2xl font-bold text-text-primary">Organizações</div>
          <div className="mt-0.5 font-sans text-sm text-text-secondary">
            {(orgs ?? []).length} organização{(orgs ?? []).length === 1 ? "" : "ões"} na plataforma
          </div>
        </div>
        {!creating && <Button onClick={() => setCreating(true)}>+ Nova organização</Button>}
      </div>

      {creating && <CreateOrgForm onClose={() => setCreating(false)} />}

      {isLoading && <div className="font-sans text-sm text-text-secondary">Carregando…</div>}
      {error && <div className="font-sans text-sm text-status-failed">{errorMessage(error, "Falha ao carregar organizações")}</div>}

      <div className="flex flex-col gap-3">
        {(orgs ?? []).map((o) => (
          <OrgRow key={o.slug} org={o} />
        ))}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Typecheck**

Run: `cd /home/kevingomes/Hatchery/web && npx tsc --noEmit 2>&1 | grep OrgsPage | head`
Expected: sem erros.

---

### Task 6: Verificação, build e handoff para validação manual

**Files:**
- Modify: `AGENTS.md`

- [ ] **Step 1: Typecheck e build completos**

Run: `cd /home/kevingomes/Hatchery/web && npx tsc --noEmit && npm run build 2>&1 | tail -15`
Expected: `tsc` sem saída; `vite build` termina com sucesso. Confirmar também que nada do frontend ainda referencia rotas antigas: `grep -rn "/gameservers/\${\(ns\|namespace\)" src/ ; grep -rn "?token=" src/ ; grep -rn "listEggs\|grantPermission\|GameServerRef" src/` — todos vazios.

- [ ] **Step 2: Entregar a lista de validação manual ao usuário (não executar sozinho)**

Informar, em português, que o frontend está pronto e que a validação visual é dele (convenção do projeto). Checklist para ele conferir, com o `panel-api` + Redis + Postgres + operator rodando:
1. Login → seletor de organização na sidebar mostra só as orgs dele; trocar de org troca a lista de servidores.
2. Errar a senha 5 vezes → mensagem "Muitas tentativas de login. Tente novamente em N min."
3. Como owner/admin: criar servidor (sem campo de namespace; Egg mostra "(catálogo)"/"(privado)"); como member: botão "+ Novo servidor" e "Excluir" não aparecem.
4. Abrir o console de um servidor: conecta (DevTools → Network: a URL do WebSocket tem `?ticket=`, **nunca** o token de sessão).
5. Aba Eggs: criar Egg privado (admin), editar, excluir; Egg em uso não exclui (mensagem de erro). Platform admin vê os botões de catálogo.
6. Membros: adicionar por username, mudar papel, remover; tentar remover o único owner mostra o erro do servidor; admin não vê a opção "Owner".
7. Auditoria (admin+): ações acima aparecem com ator, alvo e IP; "Carregar mais" pagina; member não vê o item no menu nem acessa `/audit`.
8. Plataforma → Organizações: criar org (fica "Pending"→"Active"), editar quota, excluir org com servidor (erro 409 legível). Usuários: sem seção de permissões; excluir o único owner de uma org mostra o erro.

- [ ] **Step 3: AGENTS.md**

Adicionar a seção "Multi-tenant — sub-projeto 4 (UI) implementado": seletor de org e `OrgProvider`, rotas `/orgs/:org/servers/:name`, console por ticket, páginas novas (Eggs, Membros, Auditoria, Organizações), a remoção da UI de grants, e que a validação visual foi deixada para o usuário. Atualizar a seção "Estado do MVP" e "Quantos componentes de fato rodam no cluster" (Redis novo; `Tenant` CRD; catálogo `hatchery-catalog`). Riscar do Backlog "Gestão de Eggs pela UI" e "Página de auditoria/histórico de ações".
