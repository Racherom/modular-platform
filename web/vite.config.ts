/// <reference types="vitest/config" />
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import react from '@vitejs/plugin-react'
import { fileURLToPath } from 'node:url'
import { defineConfig, type Plugin } from 'vite'

// the dev server may read only the editor itself and the bundled samples, never the rest of the checkout
const devFiles = [fileURLToPath(new URL('.', import.meta.url)), fileURLToPath(new URL('../samples', import.meta.url))]
const behindProxy = process.env.MP_DEV_PROXY === '1'

/**
 * The web image serves one bundle at the site root or under a sub path. There is no <base href>
 * (it breaks SVG url(#id) references such as edge markers): JS and CSS reference each other
 * relative to their own file (base './'), and this plugin writes the asset tags in index.html as
 * {{env `MP_BASE_PATH`}}/assets/..., which Caddy's templates fill at serve time. The dev server
 * fills the same template calls from its own environment.
 */
function placement(): Plugin {
  return {
    name: 'mp-placement',
    transformIndexHtml: {
      order: 'post',
      handler(html, ctx) {
        if (ctx.server) {
          const env: Record<string, string> = {
            MP_BASE_PATH: process.env.MP_BASE_PATH ?? '',
            MP_API_ROOT: process.env.MP_API_ROOT ?? '/planner',
          }
          return html.replace(/\{\{env `(\w+)`\}\}/g, (_, name: string) => env[name] ?? '')
        }
        return html.replace(/(src|href)="\.\/assets\//g, '$1="{{env `MP_BASE_PATH`}}/assets/')
      },
    },
  }
}

export default defineConfig({
  base: './',
  plugins: [tanstackRouter({ target: 'react' }), react(), placement()],
  build: { assetsDir: 'assets' },
  server: {
    fs: { allow: devFiles },
    // task dev: the ingress container reaches this server on the docker host and serves it on
    // the online tool's own origin, so hot reload runs where production runs
    ...(behindProxy && {
      host: '0.0.0.0',
      allowedHosts: [process.env.PLATFORM_DOMAIN ?? 'platform.localhost'],
      hmr: { protocol: 'wss', clientPort: Number(process.env.PLATFORM_HTTPS_PORT ?? 443) },
    }),
  },
  test: {
    include: ['src/**/*.test.{ts,tsx}'],
    environment: 'jsdom',
    setupFiles: ['src/test/setup.ts'],
  },
})
