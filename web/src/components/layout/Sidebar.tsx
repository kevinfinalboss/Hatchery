import { NavLink } from "react-router-dom";
import clsx from "clsx";
import { useAuth } from "../../lib/auth";
import { atLeast, useOrg } from "../../lib/org";
import { useT } from "../../lib/i18n";
import { LanguageSwitcher } from "../ui/LanguageSwitcher";
import { ThemeToggle } from "../ui/ThemeToggle";
import { Wordmark } from "../ui/Wordmark";

const navItemClasses = ({ isActive }: { isActive: boolean }) =>
  clsx(
    "flex items-center gap-2 px-3 py-1.5 font-sans text-sm transition-colors",
    isActive ? "bg-surface-hover text-primary-text" : "text-text-secondary hover:text-text-primary",
  );

function NavItem({ to, end, onNavigate, children }: { to: string; end?: boolean; onNavigate?: () => void; children: string }) {
  return (
    <NavLink to={to} end={end} onClick={onNavigate} className={navItemClasses}>
      {({ isActive }) => (
        <>
          <span aria-hidden className="w-2">
            {isActive ? ">" : ""}
          </span>
          {children}
        </>
      )}
    </NavLink>
  );
}

export function Sidebar({ onOpenPalette, onNavigate }: { onOpenPalette: () => void; onNavigate?: () => void }) {
  const { user, logout } = useAuth();
  const { orgs, current, setCurrent } = useOrg();
  const t = useT();

  return (
    <div className="flex h-full w-[240px] shrink-0 flex-col border-r border-border bg-sidebar p-4">
      <div className="px-2 pb-5 text-lg">
        <Wordmark />
      </div>

      <button
        onClick={onOpenPalette}
        className="mx-2 mb-4 flex items-center justify-between border border-border px-3 py-1.5 font-sans text-xs text-text-tertiary hover:border-border-strong hover:text-text-secondary"
      >
        <span>{t("nav.search")}</span>
        <span>Ctrl K</span>
      </button>

      {orgs.length > 0 && (
        <div className="px-2 pb-4">
          <label htmlFor="org-switcher" className="mb-1 block font-sans text-[11px] uppercase tracking-wide text-text-tertiary">
            {t("nav.organization")}
          </label>
          <select
            id="org-switcher"
            value={current?.slug ?? ""}
            onChange={(e) => setCurrent(e.target.value)}
            className="w-full border border-border-strong bg-surface px-3 py-2 font-sans text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-primary"
          >
            {orgs.map((o) => (
              <option key={o.slug} value={o.slug}>
                {o.name}
              </option>
            ))}
          </select>
        </div>
      )}

      <nav className="flex flex-col gap-0.5">
        <NavItem to="/" end onNavigate={onNavigate}>
          {t("nav.servers")}
        </NavItem>
        {current && (
          <NavItem to="/eggs" onNavigate={onNavigate}>
            {t("nav.eggs")}
          </NavItem>
        )}
        {current && (
          <NavItem to="/members" onNavigate={onNavigate}>
            {t("nav.members")}
          </NavItem>
        )}
        {atLeast(current?.role, "admin") && (
          <NavItem to="/audit" onNavigate={onNavigate}>
            {t("nav.audit")}
          </NavItem>
        )}
        {user?.isAdmin && (
          <>
            <div className="mt-4 px-3 pb-1 font-sans text-[11px] uppercase tracking-wide text-text-tertiary">{t("nav.platform")}</div>
            <NavItem to="/orgs" onNavigate={onNavigate}>
              {t("nav.orgs")}
            </NavItem>
            <NavItem to="/users" onNavigate={onNavigate}>
              {t("nav.users")}
            </NavItem>
          </>
        )}
      </nav>

      <div className="grow" />

      <div className="px-2 pb-3">
        <LanguageSwitcher />
      </div>

      <div className="flex items-center gap-2.5 border-t border-border px-2 pt-3">
        <div className="flex h-[28px] w-[28px] items-center justify-center bg-primary font-display text-sm font-bold text-on-primary">
          {user?.username.slice(0, 1).toUpperCase()}
        </div>
        <div className="flex min-w-0 flex-col">
          <span className="truncate font-sans text-sm font-semibold text-text-primary">{user?.username}</span>
          <span className="font-sans text-xs text-text-tertiary">{user?.isAdmin ? t("nav.roleAdmin") : t("nav.roleUser")}</span>
        </div>
        <div className="ml-auto flex items-center gap-3">
          <ThemeToggle />
          <button onClick={() => void logout()} className="font-sans text-xs text-text-tertiary hover:text-text-primary">
            {t("nav.logout")}
          </button>
        </div>
      </div>
    </div>
  );
}
