# OpenGuardrail product website

Public-facing English and Chinese product pages with an interactive, browser-only
assessment illustration and a documentation reader. Documentation is built
directly from the repository's Markdown files, rather than maintained as a
separate copy. Every published guide has an English and a complete Chinese version.
Missing translations or mismatched section structures fail the build. English
section anchors remain valid in Chinese pages, alongside translated anchor aliases.

The product overview and adoption guide are written for people evaluating and
integrating the service. The development roadmap remains in the repository's
contributor documentation. Its former website route redirects to the adoption
guide and is excluded from the sitemap.
The contributor roadmap translation is checked alongside published guides and
links explicitly lead to its Chinese source on GitHub.

The visual structure takes inspiration from the Open Code Review website:
dark surfaces, green accents, a product illustration, quickstart commands and
dedicated documentation. All website implementation and artwork here are original.

The public content baseline and verified release assets are recorded in
`src/release.mjs`. Keep this metadata, the download guide and the public source
documentation aligned when updating a release. The website does not query a
mutable GitHub release endpoint at runtime.

## Build and preview

Node.js 20+ and npm are required only for this website, not the Go service.

```sh
cd pages
npm ci --ignore-scripts
npm run check
npm run build
npm run preview
```

Open the printed local URL. All output is static HTML, CSS and JavaScript;
documentation stays readable without JavaScript. Without JavaScript the navigation
and all three installation panels remain visible, with a release-page download link.
The assessment illustration, platform selector, tabs and clipboard controls need
JavaScript. No API credentials, backend service or external analytics are used.
Fonts use the local system stack; rendering makes no third-party font requests.

`make test-pages` installs locked website dependencies, checks all JavaScript and
translations, and builds and validates the repository Pages path. `make test-web`
includes this gate, so the existing internal MR CI also checks the website.
The public-source exporter includes tracked `pages/` sources; local dependencies
and generated output remain untracked and excluded.

## GitHub Pages

The `deploy-pages.yml` workflow builds and deploys from `main` in the public
`alibaba/open-guardrail` repository. Pull requests build and check without deploying.
The repository's Settings → Pages → Build and deployment → Source must be set to
**GitHub Actions** once by a repository administrator. The `github-pages`
environment's configured protection rules apply.

The workflow supplies the repository subpath and canonical origin. For the
default repository URL, reproduce the build with:

```sh
SITE_BASE_PATH=/open-guardrail/ SITE_ORIGIN=https://alibaba.github.io npm run build
```

For local root previews, omit both variables. For a future custom domain, set
`SITE_ORIGIN` to its origin and `SITE_BASE_PATH=/`, and add a `CNAME` through the
normal GitHub Pages domain verification process. Do not add an unverified domain.

## Content and illustration

- `src/copy.mjs`: English and Chinese landing page content.
- `src/style.css`: shared responsive visual system and documentation styles.
- `src/demo.mjs`: deterministic illustration of the two cleanup checks.
- `src/site.js`: illustration, copy controls, mobile menu and accessible tabs.
- `build.mjs`: HTML generation, Markdown rendering and repository link rewriting.

The browser illustration never calls the service, submits evidence or operates
files. Its label remains visible beside the output. The quickstart links to the
real cleanup demo and documents its prerequisites. Roadmap items and synthetic
integrations are labeled, and the page states the verification and execution
boundaries of the current development preview.
