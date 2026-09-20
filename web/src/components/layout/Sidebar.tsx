import { NavLink } from "react-router-dom";
import clsx from "clsx";
import logo from "../../assets/logo.png";
import { useAuth } from "../../lib/auth";

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

  return (
    <div className="flex h-full w-[260px] shrink-0 flex-col border-r border-border bg-sidebar p-4">
      <div className="flex items-center gap-2.5 px-2 pb-5">
        <img src={logo} alt="Hatchery" className="h-8 w-8 rounded-lg object-cover" />
        <span className="font-display text-lg font-bold text-text-primary">Hatchery</span>
      </div>

      <nav className="flex flex-col gap-0.5">
        <NavLink to="/" end className={navItemClasses}>
          {({ isActive }) => (
            <>
              <NavDot active={isActive} />
              Servidores
            </>
          )}
        </NavLink>
        {user?.isAdmin && (
          <NavLink to="/users" className={navItemClasses}>
            {({ isActive }) => (
              <>
                <NavDot active={isActive} />
                Usuários
              </>
            )}
          </NavLink>
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
