import { useState } from "react";
import { Link, NavLink, Outlet } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";

// Top-level shell for the platform-admin console. Mounted under /admin/*. Uses
// a red accent + "PLATFORM" badge so it's visually distinct from the tenant
// dashboard (intentionally — admin actions are global and destructive).
// Deliberately does NOT render the tenant org switcher.
export default function AdminShell() {
  const { user, logout } = useAuth();
  const [userMenuOpen, setUserMenuOpen] = useState(false);

  return (
    <div className="flex min-h-full flex-col">
      <header className="border-b border-red-900/70 bg-red-950/40 backdrop-blur">
        <div className="mx-auto flex max-w-[1400px] items-center justify-between px-4 py-3">
          <div className="flex items-center gap-3">
            <div className="h-7 w-7 rounded-md bg-gradient-to-br from-red-400 to-red-700" />
            <div className="font-semibold tracking-tight">
              Bright Guard
              <span className="ml-1 text-red-400"> · Admin</span>
            </div>
            <span className="rounded-sm border border-red-700/60 bg-red-950 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-red-300">
              Platform
            </span>
          </div>

          <div className="flex items-center gap-3">
            <Link
              to="/app"
              className="rounded-md border border-slate-700 bg-slate-900 px-3 py-1 text-xs text-slate-300 hover:border-slate-500 hover:text-white"
            >
              Tenant console
            </Link>
            <div className="relative">
              <button
                onClick={() => setUserMenuOpen((v) => !v)}
                className="flex items-center gap-2 rounded-full border border-red-900 bg-red-950/60 py-1 pl-1 pr-3 text-sm hover:border-red-700"
              >
                {user?.avatarUrl ? (
                  <img
                    src={user.avatarUrl}
                    alt=""
                    className="h-6 w-6 rounded-full"
                  />
                ) : (
                  <span className="grid h-6 w-6 place-items-center rounded-full bg-red-700 text-xs font-semibold text-white">
                    {user?.email.slice(0, 1).toUpperCase()}
                  </span>
                )}
                <span className="text-slate-200">{user?.email}</span>
              </button>
              {userMenuOpen && (
                <div className="absolute right-0 z-10 mt-2 w-44 overflow-hidden rounded-md border border-slate-700 bg-slate-900 shadow-xl">
                  <button
                    onClick={() => {
                      setUserMenuOpen(false);
                      logout();
                    }}
                    className="block w-full px-3 py-2 text-left text-sm text-slate-200 hover:bg-slate-800"
                  >
                    Sign out
                  </button>
                </div>
              )}
            </div>
          </div>
        </div>
      </header>

      <div className="mx-auto flex w-full max-w-[1400px] flex-1 gap-6 px-4 py-6">
        <aside className="w-52 shrink-0">
          <nav className="flex flex-col gap-1 text-sm">
            <SideLink to="/admin" end>Overview</SideLink>
            <SideLink to="/admin/users">Users</SideLink>
            <SideLink to="/admin/orgs">Orgs</SideLink>
            <SideLink to="/admin/admins">Admins</SideLink>
            <SideLink to="/admin/audit">Audit</SideLink>
          </nav>
        </aside>
        <main className="flex-1 min-w-0">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

function SideLink({
  to,
  end,
  children,
}: {
  to: string;
  end?: boolean;
  children: React.ReactNode;
}) {
  return (
    <NavLink
      to={to}
      end={end}
      className={({ isActive }) =>
        `rounded-md px-3 py-2 transition ${
          isActive
            ? "bg-red-900/40 text-red-200 ring-1 ring-red-800"
            : "text-slate-400 hover:bg-slate-900 hover:text-slate-200"
        }`
      }
    >
      {children}
    </NavLink>
  );
}
