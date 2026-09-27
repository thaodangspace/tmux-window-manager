// @ts-check
import { defineConfig } from 'astro/config';
import starlight from '@astrojs/starlight';

import cloudflare from '@astrojs/cloudflare';

// Set SITE_URL in production when a canonical URL and sitemap are desired.
const site = process.env.SITE_URL || undefined;

export default defineConfig({
  site,

  integrations: [
    starlight({
      title: 'tmux-window-manager',
      description: 'A fuzzy tmux window switcher with event-driven coding-agent status.',
      social: [
        {
          icon: 'github',
          label: 'GitHub',
          href: 'https://github.com/thaodangspace/tmux-window-manager',
        },
      ],
      sidebar: [
        {
          label: 'Start here',
          items: [
            { label: 'Overview', slug: '' },
            { label: 'Getting started', slug: 'getting-started' },
          ],
        },
        {
          label: 'Using the plugin',
          items: [
            { label: 'Window picker', slug: 'window-picker' },
            { label: 'Agent status', slug: 'agent-status' },
            { label: 'Telegram notifications', slug: 'telegram' },
          ],
        },
        {
          label: 'Reference',
          items: [
            { label: 'Commands and configuration', slug: 'commands-and-configuration' },
            { label: 'Troubleshooting', slug: 'troubleshooting' },
            { label: 'Deploy to Cloudflare Pages', slug: 'deploy' },
          ],
        },
      ],
      customCss: ['./src/styles/custom.css'],
    }),
  ],

  adapter: cloudflare(),
});