import { NavLink } from "react-router-dom";
import clsx from "clsx";
import logo from "../../assets/logo.png";
import { useAuth } from "../../lib/auth";
import { atLeast, useOrg } from "../../lib/org";

const navItemClasses = ({ isActive }: { isActive: boolean }) =>
  clsx(
    "flex items-center gap-2.5 rounded-lg px-3.5 py-2 font-sans text-sm font-medium transition-colors",
    isActive ? "bg-surface-hover text-text-primary" : "text-text-secondary hover:text-text-primary",
  );

function NavDot({ active }: { active: boolean }) {
  return (
    <span
      className={clsx("h-1.5 w-1.5 rounded-full", active ? "bg-primary" : "bg-transparent")}
    />
  );
}

export function Sidebar() {
  const { user, logout } = useAuth();
  const { orgs, current, setCurrent } = useOrg();

  return (
    <div className="flex h-full w-[260px] shrink-0 flex-col border-r border-border bg-sidebar p-4">
      <div className="flex items-center gap-2.5 px-2 pb-5">
        <img src={logo} alt="Hatchery" className="h-8 w-8 rounded-lg object-cover" />
        <span className="font-display text-lg font-bold text-text-primary">Hatchery</span>
      </div>

      {orgs.length > 0 && (
        <div className="px-2 pb-4">
          <label htmlFor="org-switcher" className="mb-1 block font-sans text-[11px] font-medium uppercase tracking-wide text-text-tertiary">
            Organização
          </label>
          <select
            id="org-switcher"
            value={current?.slug ?? ""}
            onChange={(e) => setCurrent(e.target.value)}
            className="w-full rounded-lg border border-border-strong bg-surface px-3 py-2 font-sans text-sm text-text-primary focus:outline-none focus:ring-2 focus:ring-primary"
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
        <NavLink to="/" end className={navItemClasses}>
          {({ isActive }) => (<><NavDot active={isActive} />Servidores</>)}
        </NavLink>
        {current && (
          <NavLink to="/eggs" className={navItemClasses}>
            {({ isActive }) => (<><NavDot active={isActive} />Eggs</>)}
          </NavLink>
        )}
        {current && (
          <NavLink to="/members" className={navItemClasses}>
            {({ isActive }) => (<><NavDot active={isActive} />Membros</>)}
          </NavLink>
        )}
        {atLeast(current?.role, "admin") && (
          <NavLink to="/audit" className={navItemClasses}>
            {({ isActive }) => (<><NavDot active={isActive} />Auditoria</>)}
          </NavLink>
        )}
        {user?.isAdmin && (
          <>
            <div className="mt-4 px-3.5 pb-1 font-sans text-[11px] font-medium uppercase tracking-wide text-text-tertiary">
              Plataforma
            </div>
            <NavLink to="/orgs" className={navItemClasses}>
              {({ isActive }) => (<><NavDot active={isActive} />Organizações</>)}
            </NavLink>
            <NavLink to="/users" className={navItemClasses}>
              {({ isActive }) => (<><NavDot active={isActive} />Usuários</>)}
            </NavLink>
          </>
        )}
      </nav>

      <div className="grow" />

      <div className="flex items-center gap-2.5 border-t border-border px-2 pt-3">
        <div className="flex h-[30px] w-[30px] items-center justify-center rounded-full bg-primary font-display text-sm font-bold text-white">
          {user?.username.slice(0, 1).toUpperCase()}
        </div>
        <div className="flex min-w-0 flex-col">
          <span className="truncate font-sans text-sm font-semibold text-text-primary">{user?.username}</span>
          <span className="font-sans text-xs text-text-tertiary">
            {user?.isAdmin ? "Administrador" : "Usuário"}
          </span>
        </div>
        <button
          onClick={() => void logout()}
          className="ml-auto font-sans text-xs font-medium text-text-tertiary hover:text-text-primary"
        >
          Sair
        </button>
      </div>
    </div>
  );
}
