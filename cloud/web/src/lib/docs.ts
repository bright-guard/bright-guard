// Build-time inlining of every Markdown file under cloud/web/docs/. Vite's
// import.meta.glob with { as: 'raw', eager: true } bakes the file contents
// into the JS bundle, so the SPA needs no extra runtime fetch and the
// embedded-FS Go server doesn't need to know about .md files.
const RAW: Record<string, string> = import.meta.glob(
  "../../docs/**/*.md",
  { query: "?raw", import: "default", eager: true },
) as Record<string, string>;

// csp-integration.md is a research memo committed to docs/ before the docs
// site existed; it isn't a user doc and shouldn't show up in the sidebar or
// search index. Filter it out here, not in the file tree, so the memo can
// stay where it was for posterity.
const EXCLUDE_SUFFIX = "/docs/csp-integration.md";

export type DocPage = {
  slug: string;        // e.g. "policies/cel-primer"
  title: string;       // first H1 of the file
  headings: string[];  // every H2/H3 text in order
  body: string;        // raw markdown source
  section: string;     // top-level folder, "" for top-level pages
};

function deriveSlug(globKey: string): string {
  // globKey looks like "../../docs/policies/cel-primer.md"
  const i = globKey.indexOf("/docs/");
  const tail = globKey.slice(i + "/docs/".length);
  return tail.replace(/\.md$/, "");
}

function extractTitle(src: string, fallback: string): string {
  const m = src.match(/^#\s+(.+?)\s*$/m);
  return m ? m[1].trim() : fallback;
}

function extractHeadings(src: string): string[] {
  const out: string[] = [];
  const re = /^(#{2,3})\s+(.+?)\s*$/gm;
  let m: RegExpExecArray | null;
  while ((m = re.exec(src)) !== null) {
    out.push(m[2].trim());
  }
  return out;
}

const PAGES: DocPage[] = Object.entries(RAW)
  .filter(([k]) => !k.endsWith(EXCLUDE_SUFFIX))
  .map(([k, body]) => {
    const slug = deriveSlug(k);
    const section = slug.includes("/") ? slug.split("/")[0] : "";
    return {
      slug,
      title: extractTitle(body, slug),
      headings: extractHeadings(body),
      body,
      section,
    };
  })
  .sort((a, b) => a.slug.localeCompare(b.slug));

export function allPages(): DocPage[] {
  return PAGES;
}

export function pageBySlug(slug: string): DocPage | undefined {
  return PAGES.find((p) => p.slug === slug);
}

export type DocSection = {
  id: string;          // folder name, e.g. "policies"; "" for top-level
  label: string;       // human label
  pages: DocPage[];
};

// Order sections in a curated order; anything new gets appended after the
// known set. Top-level pages (index, etc.) come first.
const SECTION_ORDER = [
  "",
  "gateways",
  "connections",
  "policies",
  "activity-timeline",
  "cli",
  "admin",
  "reference",
];

const SECTION_LABEL: Record<string, string> = {
  "": "Overview",
  gateways: "Gateways",
  connections: "Connections",
  policies: "Policies",
  "activity-timeline": "Activity",
  cli: "CLI",
  admin: "Admin",
  reference: "Reference",
};

export function sections(): DocSection[] {
  const bySection = new Map<string, DocPage[]>();
  for (const p of PAGES) {
    const arr = bySection.get(p.section) ?? [];
    arr.push(p);
    bySection.set(p.section, arr);
  }
  const known = SECTION_ORDER.filter((id) => bySection.has(id));
  const extras = [...bySection.keys()]
    .filter((id) => !SECTION_ORDER.includes(id))
    .sort();
  return [...known, ...extras].map((id) => ({
    id,
    label: SECTION_LABEL[id] ?? id,
    pages: (bySection.get(id) ?? []).sort((a, b) => {
      // index.md or section-root pages float to the top of their section.
      const ai = a.slug.endsWith("/index") || a.slug === id ? 0 : 1;
      const bi = b.slug.endsWith("/index") || b.slug === id ? 0 : 1;
      if (ai !== bi) return ai - bi;
      return a.title.localeCompare(b.title);
    }),
  }));
}

// --- Search -----------------------------------------------------------------

// Tiny inline search. We index each page's title, headings, and body words;
// scoring weights a match in the title above headings above body. This is
// deliberately under 50 lines — fuse.js / lunr would dwarf the entire docs
// payload for the corpus size we have.
export type SearchHit = {
  page: DocPage;
  score: number;
  snippet: string;
};

function tokenize(s: string): string[] {
  return s
    .toLowerCase()
    .split(/[^a-z0-9_]+/)
    .filter((w) => w.length >= 2);
}

function snippet(body: string, terms: string[]): string {
  const lower = body.toLowerCase();
  for (const t of terms) {
    const i = lower.indexOf(t);
    if (i >= 0) {
      const start = Math.max(0, i - 40);
      const end = Math.min(body.length, i + 80);
      const head = start > 0 ? "…" : "";
      const tail = end < body.length ? "…" : "";
      return head + body.slice(start, end).replace(/\s+/g, " ").trim() + tail;
    }
  }
  return body.slice(0, 120).replace(/\s+/g, " ").trim() + "…";
}

export function search(query: string, limit = 10): SearchHit[] {
  const terms = tokenize(query);
  if (terms.length === 0) return [];
  const hits: SearchHit[] = [];
  for (const p of PAGES) {
    const titleLower = p.title.toLowerCase();
    const headingsLower = p.headings.join(" \n ").toLowerCase();
    const bodyLower = p.body.toLowerCase();
    let score = 0;
    for (const t of terms) {
      if (titleLower.includes(t)) score += 10;
      if (headingsLower.includes(t)) score += 4;
      // Count up to a handful of body matches; saturate after that so a long
      // page doesn't drown a short, more-relevant one.
      let count = 0;
      let from = 0;
      while (count < 5) {
        const i = bodyLower.indexOf(t, from);
        if (i < 0) break;
        count++;
        from = i + t.length;
      }
      score += count;
    }
    if (score > 0) {
      hits.push({ page: p, score, snippet: snippet(p.body, terms) });
    }
  }
  hits.sort((a, b) => b.score - a.score);
  return hits.slice(0, limit);
}
