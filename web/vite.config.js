import { build, defineConfig } from "vite";
import { fileURLToPath } from "node:url";

// The package bootstrap does not expose its controller. Build the installed
// public API as a classic script so first paint and interactive controls share it.
function themeBootstrap() {
  let code;
  let building;
  let base;
  async function compile() {
    const result = await build({
      configFile: false,
      publicDir: false,
      logLevel: "warn",
      build: {
        write: false,
        minify: true,
        lib: {
          entry: fileURLToPath(new URL("./src/theme-init.ts", import.meta.url)),
          formats: ["iife"],
          name: "FastCASTheme",
        },
      },
    });
    code = result[0].output[0].code;
  }
  return {
    name: "fastcas-theme-bootstrap",
    configResolved(config) {
      building = config.command === "build";
      base = config.base;
    },
    transformIndexHtml: {
      order: "post",
      handler() {
        return [{ tag: "script", attrs: { src: `${base}assets/theme-init.js` }, injectTo: "head-prepend" }];
      },
    },
    async buildStart() {
      if (!building) return;
      await compile();
      this.emitFile({ type: "asset", fileName: "assets/theme-init.js", source: code });
    },
    async configureServer(server) {
      await compile();
      server.middlewares.use((request, response, next) => {
        if (request.url !== `${base}assets/theme-init.js`) return next();
        response.setHeader("Content-Type", "text/javascript");
        response.end(code);
      });
    },
  };
}

export default defineConfig({
  base: "/console/",
  plugins: [themeBootstrap()],
  build: {
    manifest: true,
    rollupOptions: {
      input: {
        console: fileURLToPath(new URL("./index.html", import.meta.url)),
        auth: fileURLToPath(new URL("./src/auth.ts", import.meta.url)),
      },
    },
  },
  server: {
    proxy: {
      "/api": "http://127.0.0.1:8900",
      "/login": "http://127.0.0.1:8900",
      "/device": "http://127.0.0.1:8900",
      "/.well-known": "http://127.0.0.1:8900",
    },
  },
});
