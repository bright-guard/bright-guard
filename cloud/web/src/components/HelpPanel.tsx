import { useContext, useEffect, useMemo, useRef } from "react";
import { Link } from "react-router-dom";
import { marked } from "marked";
import { pageBySlug } from "../lib/docs";
import { HelpContext } from "./HelpProvider";

marked.setOptions({ gfm: true, breaks: false });

// Convert a heading like "How matches show up" into "how-matches-show-up". Mirrors
// what marked emits by default for slug ids on headings.
function slugifyHeading(s: string): string {
  return s
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9\s-]/g, "")
    .replace(/\s+/g, "-");
}

// marked doesn't add ids to headings by default; post-process the HTML so anchor
// links inside the slide-over actually have something to scroll to.
function addHeadingIds(html: string): string {
  return html.replace(
    /<(h[1-6])>([\s\S]*?)<\/\1>/g,
    (_, tag: string, body: string) => {
      const text = body.replace(/<[^>]+>/g, "");
      const id = slugifyHeading(text);
      return `<${tag} id="${id}">${body}</${tag}>`;
    },
  );
}

// Rewrite relative .md links to live under /docs/<slug>; identical rule to
// DocsPage.rewriteInternalLinks so cross-links work inside the panel.
function rewriteInternalLinks(html: string): string {
  return html.replace(
    /href="([^"]+)\.md(#[^"]*)?"/g,
    (_, path: string, frag: string | undefined) => {
      const clean = path.replace(/^\.\//, "").replace(/^\/+/, "");
      const flat = clean.replace(/^(\.\.\/)+/, "");
      return `href="/docs/${flat}${frag ?? ""}"`;
    },
  );
}

export default function HelpPanel() {
  const ctx = useContext(HelpContext);
  const state = ctx?.state ?? { open: false, slug: null, anchor: null };
  const closeHelp = ctx?.closeHelp ?? (() => undefined);
  const { open, slug, anchor } = state;

  const page = slug ? pageBySlug(slug) : undefined;
  const html = useMemo(() => {
    if (!page) return "";
    let raw = marked.parse(page.body, { async: false }) as string;
    raw = rewriteInternalLinks(raw);
    raw = addHeadingIds(raw);
    return raw;
  }, [page]);

  const scrollRef = useRef<HTMLDivElement | null>(null);

  // Escape closes; restores body scroll lock on unmount.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") closeHelp();
    };
    window.addEventListener("keydown", onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = prev;
    };
  }, [open, closeHelp]);

  // Reset scroll to top whenever a new doc opens; if an anchor is provided,
  // jump to its heading after the markdown has rendered.
  useEffect(() => {
    if (!open) return;
    const root = scrollRef.current;
    if (!root) return;
    if (anchor) {
      const el = root.querySelector<HTMLElement>(`#${CSS.escape(anchor)}`);
      if (el) {
        root.scrollTop = el.offsetTop - 16;
        return;
      }
    }
    root.scrollTop = 0;
  }, [open, slug, anchor, html]);

  return (
    <>
      <div
        aria-hidden={!open}
        onClick={closeHelp}
        className={`fixed inset-0 z-40 bg-slate-900/30 transition-opacity duration-200 ${
          open ? "opacity-100" : "pointer-events-none opacity-0"
        }`}
      />
      <aside
        role="dialog"
        aria-modal="true"
        aria-label={page ? `Help: ${page.title}` : "Help"}
        className={`fixed inset-y-0 right-0 z-50 flex w-full max-w-[560px] flex-col bg-white shadow-2xl transition-transform duration-200 ease-out sm:w-[520px] ${
          open ? "translate-x-0" : "translate-x-full"
        }`}
      >
        <header className="flex items-start justify-between gap-3 border-b border-slate-200 bg-white px-5 py-3">
          <div className="min-w-0">
            <div className="text-[11px] uppercase tracking-wider text-slate-500">
              Help
            </div>
            <div className="truncate text-sm font-semibold text-slate-900">
              {page?.title ?? (slug ? slug : "Documentation")}
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            {page && (
              <Link
                to={`/docs/${page.slug}`}
                onClick={closeHelp}
                className="rounded-md border border-slate-300 px-2 py-1 text-xs text-slate-700 hover:bg-slate-50"
              >
                Open in full docs
              </Link>
            )}
            <button
              type="button"
              onClick={closeHelp}
              aria-label="Close help"
              className="rounded-md p-1 text-slate-500 hover:bg-slate-100 hover:text-slate-900"
            >
              <svg width="18" height="18" viewBox="0 0 20 20" fill="none" aria-hidden>
                <path
                  d="M5 5l10 10M15 5L5 15"
                  stroke="currentColor"
                  strokeWidth="1.75"
                  strokeLinecap="round"
                />
              </svg>
            </button>
          </div>
        </header>
        <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto px-6 py-5">
          {page ? (
            <article
              className="docs-prose"
              dangerouslySetInnerHTML={{ __html: html }}
            />
          ) : slug ? (
            <div className="text-sm text-slate-600">
              No help page found for <code>{slug}</code>.
            </div>
          ) : (
            <div className="text-sm text-slate-600">Loading help…</div>
          )}
        </div>
      </aside>
    </>
  );
}
