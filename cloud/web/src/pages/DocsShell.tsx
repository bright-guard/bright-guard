import { useMemo, useState } from "react";
import { Link, Navigate, NavLink, Outlet, useLocation } from "react-router-dom";
import { sections, search, allPages } from "../lib/docs";

export default function DocsShell() {
  const secs = useMemo(() => sections(), []);
  const [q, setQ] = useState("");
  const hits = useMemo(() => (q.trim() ? search(q, 8) : []), [q]);
  const loc = useLocation();

  return (
    <div className="flex min-h-full flex-col bg-[var(--bg-app)]">
      <header className="w-full bg-[var(--bg-topbar)] text-[var(--text-on-dark)]">
        <div className="flex w-full items-center justify-between px-6 py-3">
          <div className="flex items-center gap-3">
            <Link to="/app" className="text-[15px] font-bold tracking-tight text-white">
              Bright Guard
            </Link>
            <span className="rounded-sm border border-white/20 bg-white/5 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wider text-[var(--text-on-dark-muted)]">
              Docs
            </span>
          </div>
          <div className="flex items-center gap-3">
            <Link
              to="/app"
              className="rounded-md border border-white/15 bg-white/5 px-3 py-1 text-[13px] text-white hover:border-white/30"
            >
              Back to app
            </Link>
          </div>
        </div>
        <div className="flex h-[3px] w-full">
          <span className="flex-1" style={{ background: "var(--csp-stripe-1)" }} />
          <span className="flex-1" style={{ background: "var(--csp-stripe-2)" }} />
          <span className="flex-1" style={{ background: "var(--csp-stripe-3)" }} />
        </div>
      </header>

      <div className="flex w-full flex-1">
        <aside className="w-[260px] shrink-0 border-r border-slate-200 bg-white">
          <div className="px-3 py-3">
            <input
              type="search"
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="Search docs…"
              className="w-full rounded-md border border-slate-300 px-2 py-1 text-sm focus:border-[var(--accent)] focus:outline-none"
            />
          </div>
          {q.trim() ? (
            <div className="px-3 pb-4">
              <div className="mb-2 text-[11px] uppercase tracking-wider text-slate-500">
                {hits.length === 0
                  ? "No matches"
                  : `${hits.length} match${hits.length === 1 ? "" : "es"}`}
              </div>
              <ul className="flex flex-col gap-1">
                {hits.map((h) => (
                  <li key={h.page.slug}>
                    <Link
                      to={`/docs/${h.page.slug}`}
                      onClick={() => setQ("")}
                      className="block rounded-md px-2 py-1 text-sm text-slate-800 hover:bg-slate-100"
                    >
                      <div className="font-medium">{h.page.title}</div>
                      <div className="line-clamp-2 text-xs text-slate-500">
                        {h.snippet}
                      </div>
                    </Link>
                  </li>
                ))}
              </ul>
            </div>
          ) : (
            <nav className="flex flex-col gap-4 px-2 pb-6 text-sm">
              {secs.map((s) => (
                <div key={s.id}>
                  <div className="px-2 pb-1 text-[11px] uppercase tracking-wider text-slate-500">
                    {s.label}
                  </div>
                  <ul className="flex flex-col">
                    {s.pages.map((p) => (
                      <li key={p.slug}>
                        <NavLink
                          to={`/docs/${p.slug}`}
                          end
                          className={({ isActive }) =>
                            `block rounded-md px-2 py-1 ${
                              isActive
                                ? "bg-[var(--accent-soft)] text-[var(--accent)]"
                                : "text-slate-700 hover:bg-slate-100"
                            }`
                          }
                        >
                          {p.title}
                        </NavLink>
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </nav>
          )}
        </aside>
        <main className="min-w-0 flex-1 bg-white px-10 py-8 text-slate-900">
          <Outlet key={loc.pathname} />
        </main>
      </div>
    </div>
  );
}

// Default landing for /docs — redirects to the index page if present.
export function DocsIndexRedirect() {
  const pages = allPages();
  if (pages.length === 0) {
    return <div className="text-slate-600">No documentation yet.</div>;
  }
  const preferred = pages.find((p) => p.slug === "index") ?? pages[0];
  return <Navigate to={`/docs/${preferred.slug}`} replace />;
}
