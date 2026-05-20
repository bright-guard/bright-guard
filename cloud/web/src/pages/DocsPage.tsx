import { useEffect, useMemo } from "react";
import { Link, useParams } from "react-router-dom";
import { marked } from "marked";
import { pageBySlug, allPages } from "../lib/docs";

// Configure marked once. GFM gets us fenced code, tables, etc. We deliberately
// don't add a sanitizer/extensions — the corpus is repo-controlled markdown
// authored by us, not tenant input.
marked.setOptions({ gfm: true, breaks: false });

export default function DocsPage() {
  const params = useParams();
  // react-router catch-all: params["*"] is the full sub-path after /docs/.
  const slug = (params["*"] ?? "").replace(/\/$/, "");
  const page = pageBySlug(slug);

  const html = useMemo(() => {
    if (!page) return "";
    let raw = marked.parse(page.body, { async: false }) as string;
    raw = rewriteInternalLinks(raw);
    return raw;
  }, [page]);

  // Scroll to top on slug change so the next page doesn't open mid-way down.
  useEffect(() => {
    window.scrollTo({ top: 0 });
  }, [slug]);

  if (!page) {
    return (
      <div>
        <h1 className="text-2xl font-semibold">Page not found</h1>
        <p className="mt-2 text-slate-600">
          No documentation page exists at <code>/docs/{slug}</code>.
        </p>
        <Link to="/docs" className="mt-4 inline-block text-[var(--accent)] underline">
          Back to docs
        </Link>
        <details className="mt-6 text-sm text-slate-500">
          <summary>Available pages</summary>
          <ul className="mt-2 list-disc pl-6">
            {allPages().map((p) => (
              <li key={p.slug}>
                <Link to={`/docs/${p.slug}`} className="text-[var(--accent)] underline">
                  {p.slug}
                </Link>
              </li>
            ))}
          </ul>
        </details>
      </div>
    );
  }

  return (
    <article
      className="docs-prose mx-auto max-w-3xl"
      dangerouslySetInnerHTML={{ __html: html }}
    />
  );
}

// Markdown files cross-link with relative paths like `./policies/cel-primer.md`
// or `../activity-timeline.md`. After marked turns those into <a href="...md">,
// rewrite the hrefs to live under /docs/<slug>. Anything that doesn't end in
// .md is left alone (external links, anchors, fragment URLs).
function rewriteInternalLinks(html: string): string {
  return html.replace(
    /href="([^"]+)\.md(#[^"]*)?"/g,
    (_, path: string, frag: string | undefined) => {
      // Drop any leading "./" or "../" — marked has already normalised them
      // relative to the file root, but we want them rooted at /docs/.
      const clean = path.replace(/^\.\//, "").replace(/^\/+/, "");
      // For "../foo" forms, strip the ../ chunks since our docs layout is flat
      // enough that the slug is unique. If a future doc cross-links across
      // siblings we'll revisit.
      const flat = clean.replace(/^(\.\.\/)+/, "");
      return `href="/docs/${flat}${frag ?? ""}"`;
    },
  );
}
