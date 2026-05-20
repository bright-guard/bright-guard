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
    <div className="flex min-h-full flex-col bg-[var(--bg-app)]">
      <header className="w-full bg-[var(--bg-topbar)] text-[var(--text-on-dark)]">
        <div className="flex w-full items-center justify-between px-6 py-3">
          <div className="flex items-center gap-3">
            <div className="h-7 w-7 rounded-md bg-gradient-to-br from-red-400 to-red-700" />
            <div className="flex items-baseline gap-2">
              <span className="text-[15px] font-bold tracking-tight text-white">
                Bright Guard
              </span>
              <span className="text-red-400">· Admin</span>
              <span className="rounded-sm border border-red-700/70 bg-red-950/60 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-red-300">
                Platform
              </span>
            </div>
          </div>

          <div className="flex items-center gap-3">
            <Link
              to="/app"
              className="rounded-md border border-white/15 bg-white/5 px-3 py-1 text-xs text-white/80 hover:border-white/30 hover:text-white"
            >
              Tenant console
            </Link>
            <div className="relative">
              <button
                onClick={() => setUserMenuOpen((v) => !v)}
                className="flex items-center gap-2 rounded-full border border-red-900/70 bg-red-950/40 py-1 pl-1 pr-3 text-[13px] text-white hover:border-red-700"
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
                <span>{user?.email}</span>
              </button>
              {userMenuOpen && (
                <div className="absolute right-0 z-10 mt-2 w-44 overflow-hidden rounded-md border border-slate-200 bg-white text-slate-800 shadow-xl">
                  <button
                    onClick={() => {
                      setUserMenuOpen(false);
                      logout();
                    }}
                    className="block w-full px-3 py-2 text-left text-sm hover:bg-slate-100"
                  >
                    Sign out
                  </button>
                </div>
              )}
            </div>
          </div>
        </div>
        {/* Admin: solid red stripe instead of the CSP brand gradient — keeps
            the "danger zone" branding while sharing the CSP top-bar shape. */}
        <div className="h-[3px] w-full bg-red-600" />
      </header>

      <div className="flex w-full flex-1">
        <aside className="w-[220px] shrink-0 bg-[var(--bg-rail)] text-[var(--text-on-dark-muted)]">
          <nav className="flex flex-col gap-0.5 px-2 py-4 text-[14px]">
            <SideLink to="/admin" end>Overview</SideLink>
            <SideLink to="/admin/users">Users</SideLink>
            <SideLink to="/admin/orgs">Orgs</SideLink>
            <SideLink to="/admin/admins">Admins</SideLink>
            <SideLink to="/admin/audit">Audit</SideLink>
          </nav>
        </aside>
        <main className="min-w-0 flex-1 px-8 py-6 text-slate-900">
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
        `rounded-md px-3 py-1.5 text-[14px] transition ${
          isActive
            ? "bg-red-900/30 text-red-200 shadow-[inset_3px_0_0_theme(colors.red.500)]"
            : "text-[var(--text-on-dark-muted)] hover:bg-white/5 hover:text-white"
        }`
      }
    >
      {children}
    </NavLink>
  );
}
