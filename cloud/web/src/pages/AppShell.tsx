import { useState } from "react";
import { NavLink, Outlet } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";

export default function AppShell() {
  const { user, memberships, activeOrgId, setActiveOrg, logout } = useAuth();
  const [userMenuOpen, setUserMenuOpen] = useState(false);

  const active =
    memberships.find((m) => m.org.id === activeOrgId) ?? memberships[0];

  return (
    <div className="flex min-h-full flex-col">
      <header className="border-b border-slate-800 bg-slate-950/70 backdrop-blur">
        <div className="mx-auto flex max-w-7xl items-center justify-between px-4 py-3">
          <div className="flex items-center gap-3">
            <div className="h-7 w-7 rounded-md bg-gradient-to-br from-brand-400 to-brand-700" />
            <div className="font-semibold tracking-tight">Bright Guard</div>
          </div>

          <div className="flex items-center gap-3">
            <select
              value={active?.org.id ?? ""}
              onChange={(e) => setActiveOrg(e.target.value)}
              className="rounded-md border border-slate-700 bg-slate-900 px-2 py-1 text-sm focus:border-brand-500 focus:outline-none"
            >
              {memberships.map((m) => (
                <option key={m.org.id} value={m.org.id}>
                  {m.org.name}
                </option>
              ))}
            </select>

            <div className="relative">
              <button
                onClick={() => setUserMenuOpen((v) => !v)}
                className="flex items-center gap-2 rounded-full border border-slate-700 bg-slate-900 py-1 pl-1 pr-3 text-sm hover:border-slate-600"
              >
                {user?.avatarUrl ? (
                  <img
                    src={user.avatarUrl}
                    alt=""
                    className="h-6 w-6 rounded-full"
                  />
                ) : (
                  <span className="grid h-6 w-6 place-items-center rounded-full bg-brand-700 text-xs font-semibold text-white">
                    {user?.email.slice(0, 1).toUpperCase()}
                  </span>
                )}
                <span className="text-slate-300">{user?.email}</span>
              </button>
              {userMenuOpen && (
                <div className="absolute right-0 z-10 mt-2 w-40 overflow-hidden rounded-md border border-slate-700 bg-slate-900 shadow-xl">
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

      <div className="mx-auto flex w-full max-w-7xl flex-1 gap-6 px-4 py-6">
        <aside className="w-52 shrink-0">
          <nav className="flex flex-col gap-1 text-sm">
            <SideLink to="/app" end>Overview</SideLink>
            <SideLink to="/app/gateways">Gateways</SideLink>
            <SideLink to="/app/mcp-servers">MCP Servers</SideLink>
            <SideLink to="/app/mcp-connections">MCP Connections</SideLink>
            <SideLink to="/app/activity">Activity</SideLink>
            <SideLink to="/app/settings/sessions">Sessions</SideLink>
          </nav>
        </aside>
        <main className="flex-1">
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
            ? "bg-slate-800 text-white"
            : "text-slate-400 hover:bg-slate-900 hover:text-slate-200"
        }`
      }
    >
      {children}
    </NavLink>
  );
}
