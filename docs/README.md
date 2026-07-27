# tmux-window-manager documentation

This is the [Starlight](https://starlight.astro.build/) documentation site for
[`tmux-window-manager`](https://github.com/thaodangspace/tmux-window-manager).
The existing screenshots and `plans/` directory remain part of the repository;
the Astro application is isolated within this directory.

## Local development

```bash
npm ci
npm run dev
```

Build and preview the production site locally with:

```bash
npm run build
npm run preview
```

The static site is written to `dist/`.

## Cloudflare Pages

Create a Pages project connected to this repository with these settings:

- **Root directory:** `docs`
- **Build command:** `npm run build`
- **Build output directory:** `dist`
- **Node.js version:** use a currently supported LTS release, or set `NODE_VERSION` in the Pages environment variables

Cloudflare Pages installs dependencies from `package-lock.json` before running
the build. No Cloudflare adapter or runtime functions are needed because Astro
emits static HTML. Set `SITE_URL` to the canonical public URL to emit canonical
links and a sitemap; it is optional for deployment.

If the Pages project must use the repository root as its root directory, use:

- **Build command:** `npm --prefix docs ci && npm --prefix docs run build`
- **Build output directory:** `docs/dist`
