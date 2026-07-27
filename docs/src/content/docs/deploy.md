---
title: Deploy to Cloudflare Pages
description: Publish the static tmux-window-manager documentation site with Cloudflare Pages.
---

The documentation is a static Astro build. It does not need a server adapter,
Cloudflare Worker, runtime functions, access to tmux, or access to the status
database. Set `SITE_URL` only when a canonical public URL is known; Astro then
emits canonical links and a sitemap.

## Configure a Pages project

Connect the repository to Cloudflare Pages with these settings:

| Setting | Value |
| --- | --- |
| Root directory | `docs` |
| Build command | `npm run build` |
| Build output directory | `dist` |

Cloudflare installs dependencies from `docs/package-lock.json`, runs Astro, and
publishes the generated `dist/` directory. Use a currently supported Node.js LTS
release. Set `NODE_VERSION` in the Pages environment if the default does not
satisfy Astro's runtime requirement.

The included `public/_headers` file adds `X-Content-Type-Options: nosniff` and a
strict-origin referrer policy to deployed responses.

## Verify before publishing

```sh
cd docs
npm ci
npm run build
npm run preview
```

From the repository root, the equivalent build shortcut is:

```sh
make docs-build
```

To verify canonical and sitemap output locally:

```sh
SITE_URL=https://docs.example.com npm run build
```

:::tip[Repository-root alternative]
If the Pages project must keep the repository root as its root directory, use
`npm --prefix docs ci && npm --prefix docs run build` as the build command and
`docs/dist` as the output directory.
:::

Generated `.astro/`, `node_modules/`, and `dist/` directories are local build
artifacts and are not committed.
