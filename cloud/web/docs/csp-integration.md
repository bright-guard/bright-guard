# CSP look-and-feel and micro-frontend integration for Bright Guard

Status: research memo (read-only investigation). No code changes have been
made. Author: dgarcia@infoblox.com. Date: 2026-05-20.

## 1. Scope and method

The user asked: can the Bright Guard UI (`cloud/web`) look and feel like
Infoblox's CSP root UI, with an eye toward eventually loading Bright Guard
**inside** the CSP shell as a micro-frontend?

I surveyed three Infoblox-CTO private repos (read-only, via `gh repo clone`
with the `daniel-garcia` token):

| Repo | Access | What it is |
|------|--------|------------|
| `Infoblox-CTO/csp.root.ui` | yes | The CSP shell — nginx server + JS loader for micro-frontends |
| `Infoblox-CTO/csp.pds-core.ui` | yes | Reference CSP product UI (PDS Core demo + the `@infoblox-cto/pds-core` Angular library) |
| `Infoblox-CTO/pds-core` | yes, but empty | A near-empty repo (README, repo-info.yaml). It is the static-site landing for the PDS Core demo, not a backend. |

The current Bright Guard UI is at `cloud/web/`: Vite 5 + React 18 +
TypeScript + Tailwind v3 + react-router v6. Entry is `src/main.tsx`,
layout is `src/pages/AppShell.tsx`.

## 2. The single most important finding

**CSP root is a Single-SPA host, not Module Federation.** Every existing
CSP child app is an Angular 15 application built with Webpack 5 and the
`@infoblox-cto/ufe-angular` Angular CLI schematics on top of
`single-spa-angular`. The host loads child bundles at runtime via
**SystemJS import maps**, driven by **feature flags**. There is no React
example, no Vite example, no Module Federation in any of the three repos.

Concretely:

- `csp.root.ui/packages/root-ui/package.json` depends on
  `single-spa@^5.3.4`, `systemjs@6.3.1`, and `import-map-overrides@^1.15.0`.
- `csp.pds-core.ui/package.json` depends on `single-spa-angular@^8.1.1`,
  `systemjs-webpack-interop@^2.3.7`, and `@angular/*@^15.2.x`.
- The single source of truth for what loads is **LaunchDarkly-style feature
  flags** named `ufe-<app>-version` (see
  `csp.root.ui/packages/root-ui/src/root-ui.js#getAppDefs`). The shell
  fetches the flag list, asks the CDN for `metadata.js` per UFE, builds
  the SystemJS import map, then `registerApplication(...)`.

This dictates the micro-frontend recommendation in section 6.

## 3. CSP visual identity (extracted from the source)

### Top-level shell — `csp.root.ui/packages/root-ui/src/index.ejs`

The host DOM is a fixed set of named mount-point divs:

```html
<csp-loading-spinner id="csp-spinner"></csp-loading-spinner>
<csp-support-banner ...></csp-support-banner>
<csp-sandbox-banner ... id="csp-sandbox-banner"></csp-sandbox-banner>
<div id="daemon-processes"></div>
<div class="content-wrap">
  <div id="page-top">
    <div id="page-top-left">
      <div id="ib-logo"></div>
      <csp-msp-tenant ...></csp-msp-tenant>
      <div id="tenant-selector"></div>
    </div>
    <div id="page-top-right">
      <div id="compartment-selector"></div>
      <div id="global-search"></div>
      <div id="notifications"></div>
      <div id="user-menu"></div>
    </div>
  </div>
  <div id="page-main">
    <div id="main-nav"></div>
    <div id="main-content">
      <div id="alert-panel"></div>
      <div id="page-title"></div>
      <div id="top-nav"></div>
      <div id="app-nav"></div>
      <div id="app-content"></div>
    </div>
  </div>
</div>
```

A UFE renders into `#app-content` (the application body) and can
contribute nav menu entries via its `metadata.js`. `nav-menu` itself is a
separate UFE that owns `#main-nav`.

### Brand colors — `csp.root.ui/packages/root-ui/src/style/index.css`

```css
.ib-default-theme #page-top {
  background: linear-gradient(to bottom right, #101820 40%, rgba(16, 24, 32, 0.85) 90%);
  border-bottom: 2px solid;
  border-image: linear-gradient(
      90deg,
      rgba(254, 221, 0, 0.9) 0%,    /* Infoblox yellow */
      rgba(0, 189, 77, 0.9) 35%,    /* Infoblox green */
      rgba(0, 189, 77, 0.9) 65%,
      rgba(0, 226, 236, 0.9) 100%   /* Infoblox cyan */
    )
    1;
}
```

- **Brand stripe:** yellow `#FEDD00` -> green `#00BD4D` -> cyan `#00E2EC`.
- **Top bar background:** near-black `#101820`.
- **MSP top-bar variant:** `linear-gradient(to bottom right, #1976D2 40%, rgba(2, 87, 171, 1) 90%)` — blue. (`root-ui.js#getEntitlements`)
- **Body font:** `Lato`, base size `12px`, with weights 300/400/700. Bundled as `.ttf` under `assets/webfonts/`.

### App-level (inside `#app-content`) — PDS Core design tokens

`csp.pds-core.ui/projects/pds-core/scss/colors/theme/palette/palette-default.scss`
defines the canonical CSS-custom-property palette. Highlights:

```scss
// Brand
$ib-pds-core-bg-brand-bold-color:         var(--ib-pds-core-bg-brand-bold-color,         #0171da);
$ib-pds-core-bg-brand-bold-hover-color:   var(--ib-pds-core-bg-brand-bold-hover-color,   #0e5aa1);
$ib-pds-core-bg-brand-subtle-color:       var(--ib-pds-core-bg-brand-subtle-color,       #ebf5ff);

// Text
$ib-pds-core-text-primary-color:    var(--ib-pds-core-text-primary-color,    #212121);
$ib-pds-core-text-secondary-color:  var(--ib-pds-core-text-secondary-color,  #616161);
$ib-pds-core-text-link-color:       var(--ib-pds-core-text-link-color,       #0277bd);

// Semantic
$ib-pds-core-text-success-color:  #006128;
$ib-pds-core-text-warning-color:  #a97f13;
$ib-pds-core-text-critical-color: #c05700;
$ib-pds-core-text-alert-color:    #b71c1c;

// Gradients (re-used from shell)
$ib-pds-core-bg-gradient:
  linear-gradient(90deg, #fedd00 0%, #00bd4d 35%, #00bd4d 65%, #00e2ec 100%);
```

So inside an app body, "brand" actually means **CSP blue `#0171da`**, not
the green/yellow/cyan brand stripe — that gradient is reserved for shell
chrome and section dividers. Status text uses muted, accessible colors
(`#006128`, `#b71c1c`).

### Component library

`csp.pds-core.ui/projects/pds-core/src/lib/` is the Infoblox-published
NPM package `@infoblox-cto/pds-core` (Angular 15 + Material 15 + CDK). It
ships ~60 components: `button`, `cards`, `badges`, `chip`, `dialog`,
`dropdown`, `filter-search-bar`, `list-table`, `navigation-menu`,
`pds-dropdown`, `perspective-nav`, `side-nav-layout`, `slide-out-panel`,
`spinner`, `table`, `tabs`, `wizard`, etc.

It is Angular-only. There is **no React or framework-neutral build
target** in the repo today.

### Typography and density

- Base size **12px** at `<html>` level (declared in
  `root-ui/src/style/index.css`). Apps inherit this — CSP UIs are noticeably
  denser than typical web apps.
- Font family `Lato, sans-serif`, served as `.ttf` from the shell.
- `box-sizing: border-box` global, zeroed margin/padding.

## 4. Micro-frontend mechanism

### Discovery and loading

From `csp.root.ui/packages/root-ui/src/root-ui.js`:

1. The shell fetches feature flags from `/api/terminus/v1/features`.
2. `findUfeVersions(features)` extracts every flag named `ufe-<id>-version`.
   Its `value` is the version tag deployed on the CDN.
3. For each enabled UFE, the shell fetches
   `<CDN_URL>/<id>/<version>/metadata.js` (plain `require` over SystemJS).
4. `metadata.exports` declares lifecycle entry points (typically a single
   `{ id: 'pds-core', entry: 'main.js', type: 'ufe-application' }`).
5. The shell builds the SystemJS import map (key
   `@infoblox-csp/<id>` -> URL) and calls Single-SPA `registerApplication`.

The custom props passed to every UFE (`root-ui.js` lines 444-451):

```js
registerApplication(
  exp.appDef.systemName,
  () => System.import(exp.appDef.systemName),
  routerFn(appId),
  { path, sessionToken, navItems, navGroups, searchObjects, lrtTasks },
);
```

### UFE contract

A UFE must publish **two** files on CDN at
`<CDN_URL>/<id>/<version>/`:

- **`metadata.js`** (declarative): exports `{ navItems, navGroups,
  searchObjects, routes, lrtTasks, exports }`. Used by the shell *before*
  it even loads the UFE bundle.
- **`main.js`** (runtime): exports Single-SPA lifecycle functions
  `bootstrap`, `mount`, `unmount`.

Reference implementation from
`csp.pds-core.ui/projects/pds-core-demo/src/metadata.ts`:

```ts
const exportList = [
  { id: 'pds-core', entry: 'main.js', type: 'ufe-application' },
];
export default {
  navGroups: [{ group: 'pds-core-ufe', name: 'Demo', itemClass: 'nav-icon far fa-book', ... }],
  navItems:  [{ group: 'pds-core-ufe', url: '/pds-core-demo', name: 'PDS Core', weight: 1000, ... }],
  exports: exportList,
};
```

And `csp.pds-core.ui/projects/pds-core-demo/src/main.ts`:

```ts
const lifecycles = singleSpaAngular({
  bootstrapFunction: (rootUiProps: RootUiProps) => {
    const { path, sessionToken } = rootUiProps;
    setPublicPath(path);
    return platformBrowserDynamic([
      { provide: RootUiSessionTokenService, useValue: sessionToken },
    ]).bootstrapModule(AppModule);
  },
  template: '<ib-pds-core-demo/>',
  NgZone,
  domElementGetter: () => document.getElementById('app-content'),
});
export const { bootstrap, mount, unmount } = lifecycles;
```

`RootUiProps` is:

```ts
export type RootUiProps = AppProps & {
  path: string;
  sessionToken: () => Promise<string>;
};
```

So the host provides: (a) the deployed bundle base URL (`path`) and (b) a
**function** that returns a promise of the current JWT. Identity is
delegated through Okta + `/v2/session/users/*` endpoints owned by root-ui;
child apps do **not** run their own login flow.

### Local development override

`root-ui-helper.js` honors `import-map-overrides` entries stored in
localStorage. `ufeOverride("my-ufe", "https://localhost:4200/")` redirects
the shell to a developer's local dev server — no shell deploy needed.

### Shared dependencies

Per `site/content/integration/shared-deps.md`, root-ui vendors a fixed set
of shared libs (`pdfmake`, `moment`, `html2canvas`, `canvg`, `vfs_fonts`)
at `/shared/shared-app-deps/`. UFEs mark these as Webpack `externals` so
they aren't double-bundled. **React is not on the shared list** — if a
React UFE were added, it would either ship its own React or the shell
would need to start vendoring one.

## 5. Where Bright Guard stands today

`cloud/web/package.json` (excerpt):

```json
"dependencies": {
  "react": "^18.3.1",
  "react-dom": "^18.3.1",
  "react-router-dom": "^6.27.0"
},
"devDependencies": {
  "vite": "^5.4.10",
  "tailwindcss": "^3.4.14"
}
```

`cloud/web/tailwind.config.js` brand palette is cyan-tinted (`#06b6d4`
ish), Tailwind defaults otherwise. `AppShell.tsx` uses a dark
slate-on-near-black look (`bg-slate-950/70`, `border-slate-800`,
`text-slate-400`) with a constrained `max-w-7xl` two-pane layout. That
already aligns aesthetically with CSP's dark top-bar, but it does **not**
use CSP's color tokens, type scale, or DOM structure.

Important framework mismatch: **CSP standardizes on Angular**. Every
piece of tooling — `ufe-angular` schematics, `single-spa-angular`,
`@infoblox-cto/pds-core` Angular library — assumes Angular. Adopting CSP
*tooling* unchanged would mean rewriting in Angular.

## 6. Recommendation

I am ordering this in three waves of increasing scope. Each wave is
independently shippable.

### Wave 1 — visual rebrand (1-2 days)

Cheap, fully reversible, no architectural commitment. Goals:

1. **Replace the Tailwind brand palette** with CSP brand tokens. Map
   Tailwind's `brand` scale to shades around `#0171da` (CSP app-body brand
   blue). Add semantic colors `success: #006128`, `warning: #a97f13`,
   `critical: #c05700`, `alert: #b71c1c`. Add `infoblox-yellow #FEDD00`,
   `infoblox-green #00BD4D`, `infoblox-cyan #00E2EC` for accents.
2. **Switch the body font to Lato** (Google Fonts). Drop base font-size
   to `13-14px` (12px is CSP's density target; 12px in Tailwind feels too
   tight on a marketing-adjacent product like Bright Guard).
3. **Restructure `AppShell.tsx`** to mirror the CSP shell DOM: a dark
   `#101820` top bar with the yellow->green->cyan border-image bottom
   stripe, left-aligned product logo, right-aligned org/tenant selector +
   user menu, then a left rail nav + main content. Drop `max-w-7xl` — CSP
   layouts are full-width.
4. **Adopt CSP table/badge styling patterns.** Take the PDS Core token
   names (`text-secondary`, `bg-container-subtle`, etc.) as the mental
   model and define equivalent Tailwind utility classes. Don't import the
   Angular SCSS — re-implement the small subset Bright Guard actually
   uses.

Outcome: Bright Guard "looks like CSP" to an Infoblox employee, with no
new dependencies and no framework changes.

### Wave 2 — design-token alignment (1-2 weeks)

1. **Vendor a tokens layer.** Author
   `cloud/web/src/styles/csp-tokens.css` that defines the
   `--ib-pds-core-*` CSS custom properties Bright Guard cares about,
   sourcing the values from
   `csp.pds-core.ui/projects/pds-core/scss/colors/theme/palette/palette-default.scss`.
   Hand-port (do not copy entire files — they are proprietary). Light
   theme first. Use the tokens in `tailwind.config.js` via Tailwind's
   `theme.colors = { 'brand': 'var(--ib-pds-core-bg-brand-bold-color)' }`
   pattern.
2. **Re-implement Bright Guard's core primitives** (button, table, badge,
   tabs, dialog, chip, side-nav, toast) against those tokens. Stay React +
   Tailwind; **do not** introduce Angular Material. Use Headless UI or
   Radix Primitives for behavior; keep visual design driven by tokens.
3. **Match icon system.** PDS Core uses Font Awesome (`fa-book`, etc.) +
   a small custom icon font (`$ib-pds-icn-*`). Bright Guard should pick
   one set and stay disciplined. Lucide is the closest free, MIT, React-
   friendly option and visually compatible with FA-thin.
4. **Light + dark theme parity.** PDS Core ships
   `light-theme-mixin.scss` and `dark-theme-mixin.scss`. Use CSS variables
   so Bright Guard can flip a `data-theme` attribute.

Outcome: Bright Guard's UI is visually indistinguishable from a CSP
product UI for someone who isn't squinting at pixels.

### Wave 3 — actual micro-frontend integration (1-2 months, conditional)

This is where the framework mismatch bites. Three plausible paths:

#### Path A (recommended) — Single-SPA wrapper around the existing React app

This is the closest match to what CSP already expects.

1. **Add a Single-SPA lifecycle adapter.** Use the official
   [`single-spa-react`](https://single-spa.js.org/docs/ecosystem-react/)
   package (mirror of `single-spa-angular`). It is well-supported, used in
   production at many shops, and the protocol is identical from the
   shell's perspective.
2. **Build a second entry point next to the Vite dev server.** Vite
   builds the SPA today; the UFE entry must be a SystemJS bundle.
   `@originjs/vite-plugin-federation` is the wrong tool here — we need
   SystemJS output. The pragmatic choice is to introduce a separate
   Webpack 5 + `systemjs-webpack-interop` build *only* for the UFE bundle
   (`main.js` + `metadata.js`), while keeping Vite for everyday dev. This
   matches what PDS Core does (Angular CLI for dev, custom Webpack for
   UFE).
3. **Implement `metadata.ts`.** Declare `navItems`, `routes`, and a
   single `exports` entry `{ id: 'bright-guard', entry: 'main.js', type:
   'ufe-application' }`. Pick a URL prefix (e.g. `/bright-guard`) and a
   nav group.
4. **Implement `main.ts` with the React lifecycle.** Mount point is
   `document.getElementById('app-content')` (the standard CSP slot). Pull
   the JWT from `rootUiProps.sessionToken()` and feed it to Bright
   Guard's existing `AuthContext` instead of the current login flow.
5. **Suppress shell chrome in micro-frontend mode.** `AppShell.tsx`'s
   top bar and side nav must disappear when running embedded — the shell
   owns those. Gate them on a `isStandalone` prop.
6. **Talk to platform.** Bright Guard needs a `ufe-bright-guard-version`
   feature flag created in CSP's LaunchDarkly tenant and a CDN
   deployment slot. This is org work, not engineering work, but it is the
   actual gate.
7. **Auth wire-up.** The shell hands the UFE a JWT. The Bright Guard API
   today expects its own session cookie. Bridge code is required, either
   server-side (accept the CSP JWT) or client-side (exchange CSP JWT for a
   Bright Guard token).

#### Path B — Web Component / iframe escape hatch

If Single-SPA + SystemJS proves too invasive, the shell can host a
`<rui-ufe-component>` wrapping an iframe of Bright Guard. The CSP shell
has a built-in `rui-ufe-component` (see
`csp.root.ui/packages/root-ui/src/components/rui-ufe-component/`).
Iframes are weak on shared chrome, drag-and-drop, and global keyboard
shortcuts, but require zero changes to Bright Guard's build. Use as a
**fallback**, not a target state.

#### Path C — rewrite in Angular

The only path that lets Bright Guard consume `@infoblox-cto/pds-core`
directly. Rejected: it is a multi-quarter rewrite that throws away the
existing UI investment for marginal benefit. A token-aligned React app
shipping its own primitives is visually equivalent.

**Pick Path A.** It matches CSP's existing contract, doesn't require a
rewrite, and is the same pattern used at other large Single-SPA shops
(Lucid, ZocDoc) that run mixed Angular + React under one host.

## 7. What to keep, what to throw away

Keep:

- `cloud/web/src/api/` and `cloud/web/src/auth/` (AuthContext) — the
  auth layer needs a `sessionToken` adapter but the shape is right.
- `react-router-dom` v6 — Single-SPA supports nested routers; the
  in-app routes between pages stay.
- The page components themselves (`OverviewPage`, `GatewaysPage`, etc.) —
  they are presentation, framework-neutral on the inside.

Restructure:

- `AppShell.tsx` — split into `StandaloneShell` (current behavior) and
  `EmbeddedShell` (no top bar, no side nav, just `<Outlet />`). Toggle on
  build flag or runtime prop.
- `tailwind.config.js` — replace brand palette with CSP tokens (Wave 1).
- `index.html` — when built for UFE, root element must be `#app-content`
  (matching CSP shell), not `#root`.

Throw away:

- Hard-coded `max-w-7xl` constraints. CSP layouts use full viewport
  width.
- The cyan brand palette in Tailwind config — replace.
- The login page mounting logic when running embedded — the shell owns
  auth.

## 8. What could go wrong (risks)

1. **Framework lock-in is real.** Every CSP-published library
   (`@infoblox-cto/pds-core`, `@infoblox-cto/ufe-angular`) is Angular. The
   moment Bright Guard wants a CSP-published component, it has to either
   re-implement it in React or wrap the Angular element in a Web
   Component (possible but ugly). Stay React-only as long as visual
   parity is sufficient.
2. **Shell churn.** Single-SPA 5.x is mature, but the root-ui code uses
   pinned-old versions (`rxjs 5.5.12`, `systemjs 6.3.1`, `webpack 5`,
   Node 18). A Bright Guard UFE bundle has to coexist with whatever the
   root-ui shell currently understands. Expect occasional version-pin
   negotiations.
3. **Internal NPM registry dependency.** `@infoblox-cto/ufe-angular`
   ships via `https://npm.pkg.github.com/infoblox-cto`. Even if Bright
   Guard avoids it, any future "share a CSP banner / shell primitive"
   work requires CI access to that registry.
4. **Auth bridging.** Today Bright Guard issues its own session.
   Inside CSP, the shell hands you a JWT and expects you to use it. The
   backend (`cloud/api`) currently has no path to accept a CSP JWT; that
   is a real Go change in scope.
5. **CDN deployment.** The shell loads UFE bundles from a specific CDN
   path `<CDN_URL>/<id>/<version>/`. Bright Guard's current deploy is to
   `https://mcp-governance.infoblox.dev` (self-hosted Vite static build).
   To register as a CSP UFE, the bundle also needs to land on the CSP
   CDN — different deploy pipeline.
6. **Feature flag gating.** UFEs are listed via flags in CSP's
   LaunchDarkly tenant. We do not own that tenant. Coordination with
   platform required for both staging and prod.
7. **Density mismatch.** CSP runs at 12px base. Bright Guard at 14px+
   will feel "tall" inside the CSP shell. Either drop density (cheap) or
   accept the visual seam (callable-out).
8. **Routing collisions.** CSP uses hash routing (`location.hash`,
   `single-spa:routing-event`). Bright Guard uses `BrowserRouter` (path
   routing). When embedded, Bright Guard must switch to `HashRouter` (or
   use a memory router scoped to a path prefix).
9. **Single-SPA + React Strict Mode + Suspense + concurrent rendering.**
   Single-SPA's lifecycle is imperative (`mount`/`unmount`); React 18
   concurrent features can fight it. Workable but expect edge cases.

## 9. Open questions for the platform team

- Is there an internal "React UFE" precedent inside Infoblox we missed?
  None visible in the three repos we surveyed.
- What is the canonical way to bridge CSP Okta JWTs into a non-Angular
  backend?
- Does CSP plan to publish a framework-neutral token package (CSS
  variables only, no Angular) we could vendor?
- Is there a "sandbox" or staging CSP environment where a Bright Guard
  UFE can be exercised before production gating?

## 10. References

All paths below are inside the shallow clones at `/tmp/csp-research/`:

- `csp.root.ui/README.md` — overview of root-ui and `ufeOverride()`
- `csp.root.ui/packages/root-ui/src/index.ejs` — shell DOM contract
- `csp.root.ui/packages/root-ui/src/root-ui.js` — UFE discovery,
  registration, lifecycle
- `csp.root.ui/packages/root-ui/src/helper/root-ui-helper.js` — metadata
  parser + import-map builder
- `csp.root.ui/packages/root-ui/src/style/index.css` — shell typography
  and chrome
- `csp.root.ui/packages/ufe-schematics/README.md` — Angular schematics
  for creating a new UFE
- `csp.root.ui/site/content/integration/ufe-lifecycle.md` — published
  integration guide (sequence diagrams, contract)
- `csp.root.ui/site/content/integration/shared-deps.md` — SystemJS
  shared-deps mechanism
- `csp.pds-core.ui/package.json` — reference UFE dependencies
- `csp.pds-core.ui/projects/pds-core-demo/src/main.ts` — UFE Single-SPA
  entry point
- `csp.pds-core.ui/projects/pds-core-demo/src/metadata.ts` — UFE
  metadata contract
- `csp.pds-core.ui/projects/pds-core-demo/src/ufe/root-ui-props.ts` —
  host -> child props type
- `csp.pds-core.ui/projects/pds-core/scss/colors/theme/palette/palette-default.scss`
  — canonical color tokens
