import { useEffect, useState } from "react";
import { Link, NavLink, Outlet } from "react-router-dom";
import { useAuth } from "../auth/AuthContext";
import { api } from "../api/client";
import type { Invitation } from "../api/types";

export default function AppShell() {
  const { user, memberships, activeOrgId, setActiveOrg, logout } = useAuth();
  const [userMenuOpen, setUserMenuOpen] = useState(false);
  const [pendingInvites, setPendingInvites] = useState<Invitation[]>([]);

  const active =
    memberships.find((m) => m.org.id === activeOrgId) ?? memberships[0];

  // Poll once on mount — pending invites are low-frequency and the banner can
  // wait for the next page navigation to refresh.
  useEffect(() => {
    api<Invitation[]>("/api/me/invitations")
      .then((list) => setPendingInvites(list ?? []))
      .catch(() => setPendingInvites([]));
  }, [activeOrgId]);

  return (
    <div className="flex min-h-full flex-col bg-[var(--bg-app)]">
      <header className="w-full bg-[var(--bg-topbar)] text-[var(--text-on-dark)]">
        <div className="flex w-full items-center justify-between px-6 py-3">
          <div className="flex items-center gap-3">
            <BrandMark />
            <div className="flex items-baseline gap-2">
              <span className="text-[15px] font-bold tracking-tight text-white">
                Bright Guard
              </span>
              <span className="rounded-sm border border-white/20 bg-white/5 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-[var(--text-on-dark-muted)]">
                Platform
              </span>
            </div>
          </div>

          <div className="flex items-center gap-3">
            <select
              value={active?.org.id ?? ""}
              onChange={(e) => setActiveOrg(e.target.value)}
              className="rounded-md border border-white/15 bg-white/5 px-2 py-1 text-[13px] text-white focus:border-[var(--accent)] focus:outline-none"
            >
              {memberships.map((m) => (
                <option key={m.org.id} value={m.org.id} className="text-slate-900">
                  {m.org.name}
                </option>
              ))}
            </select>

            <div className="relative">
              <button
                onClick={() => setUserMenuOpen((v) => !v)}
                className="flex items-center gap-2 rounded-full border border-white/15 bg-white/5 py-1 pl-1 pr-3 text-[13px] text-white hover:border-white/30"
              >
                {user?.avatarUrl ? (
                  <img
                    src={user.avatarUrl}
                    alt=""
                    className="h-6 w-6 rounded-full"
                  />
                ) : (
                  <span
                    className="grid h-6 w-6 place-items-center rounded-full text-xs font-semibold text-white"
                    style={{ background: "var(--accent)" }}
                  >
                    {user?.email.slice(0, 1).toUpperCase()}
                  </span>
                )}
                <span>{user?.email}</span>
              </button>
              {userMenuOpen && (
                <div className="absolute right-0 z-10 mt-2 w-40 overflow-hidden rounded-md border border-slate-200 bg-white text-slate-800 shadow-xl">
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
        <BrandStripe />
      </header>

      {pendingInvites.length > 0 && (
        <div className="border-b border-[var(--accent)]/40 bg-[var(--accent-soft)] text-slate-800">
          <div className="flex w-full items-center justify-between gap-4 px-6 py-2 text-[13px]">
            <span>
              You have {pendingInvites.length} pending invitation
              {pendingInvites.length === 1 ? "" : "s"}.
            </span>
            <Link
              to={`/app/invitations/${pendingInvites[0].id}`}
              className="rounded-md border border-[var(--accent)] bg-white px-3 py-1 text-xs text-[var(--accent)] hover:bg-[var(--accent)] hover:text-white"
            >
              Review
            </Link>
          </div>
        </div>
      )}

      <div className="flex w-full flex-1">
        <aside className="w-[220px] shrink-0 bg-[var(--bg-rail)] text-[var(--text-on-dark-muted)]">
          <nav className="flex flex-col gap-0.5 px-2 py-4 text-[14px]">
            <SideLink to="/app" end>Overview</SideLink>
            <SideLink to="/app/gateways">Gateways</SideLink>
            <SideLink to="/app/mcp-servers">MCP Servers</SideLink>
            <SideLink to="/app/mcp-connections">MCP Connections</SideLink>
            <SideLink to="/app/activity">Activity</SideLink>
            <SideLink to="/app/callers">Callers</SideLink>
            <SideLink to="/app/settings/members">Members</SideLink>
            <SideLink to="/app/settings/sessions">Sessions</SideLink>
          </nav>
        </aside>
        <main className="min-w-0 flex-1 px-8 py-6 text-slate-900">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

function BrandMark() {
  // Small inline SVG square in the CSP cyan-blue accent; no new deps.
  return (
    <svg
      viewBox="0 0 28 28"
      width="28"
      height="28"
      aria-hidden="true"
      className="shrink-0"
    >
      <rect width="28" height="28" rx="6" fill="var(--accent)" />
      <path
        d="M8 9h6a4 4 0 0 1 0 8H8V9zm0 0h6a4 4 0 0 1 0 8H8m0-4h7"
        stroke="white"
        strokeWidth="2"
        strokeLinecap="round"
        fill="none"
      />
    </svg>
  );
}

function BrandStripe() {
  // Three stacked spans for the CSP yellow/green/cyan stripe under the top bar.
  return (
    <div className="flex h-[3px] w-full">
      <span className="flex-1" style={{ background: "var(--csp-stripe-1)" }} />
      <span className="flex-1" style={{ background: "var(--csp-stripe-2)" }} />
      <span className="flex-1" style={{ background: "var(--csp-stripe-3)" }} />
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
            ? "bg-white/5 text-white shadow-[inset_3px_0_0_var(--accent)]"
            : "text-[var(--text-on-dark-muted)] hover:bg-white/5 hover:text-white"
        }`
      }
    >
      {children}
    </NavLink>
  );
}
