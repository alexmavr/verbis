import type { ForgeConfig } from '@electron-forge/shared-types'
import { MakerZIP } from '@electron-forge/maker-zip'
import { AutoUnpackNativesPlugin } from '@electron-forge/plugin-auto-unpack-natives'
import { WebpackPlugin } from '@electron-forge/plugin-webpack'
import * as path from 'path'
import * as fs from 'fs'

import { mainConfig } from './webpack.main.config'
import { rendererConfig } from './webpack.renderer.config'

const packageJson = JSON.parse(fs.readFileSync(path.resolve(__dirname, './package.json'), 'utf8'))

const config: ForgeConfig = {
  packagerConfig: {
    appVersion: process.env.VERSION || packageJson.version,
    asar: true,
    icon: "./assets/icon.icns",
    extraResource: [
      "../dist/verbis",
      "../dist/ollama",
      "../dist/weaviate",
      "../dist/lib",
      "../dist/pdftotext",
      "../dist/credentials.json",
      "../dist/Modelfile.custom-mistral",
      "../dist/rerank",
      //      path.join(__dirname, './assets/iconTemplate.png'),
    ],
    ...(process.env.SIGN
      ? {
          osxSign: {
            identity: process.env.APPLE_IDENTITY,
          },
          osxNotarize: {
            tool: "notarytool",
            appleId: process.env.APPLE_ID || "",
            appleIdPassword: process.env.APPLE_PASSWORD || "",
            teamId: process.env.APPLE_TEAM_ID || "",
          },
        }
      : {}),
    osxUniversal: {
      x64ArchFiles: "**/*,!**/*.dylib",
    },
  },
  rebuildConfig: {},
  makers: [new MakerZIP({}, ["darwin"])],
  hooks: {
    readPackageJson: async (_, packageJson) => {
      return {
        ...packageJson,
        version: process.env.VERSION || packageJson.version,
      };
    },
  },
  plugins: [
    new AutoUnpackNativesPlugin({}),
    new WebpackPlugin({
      mainConfig,
      devContentSecurityPolicy: `default-src * 'unsafe-eval' 'unsafe-inline'; img-src data: 'self'`,
      renderer: {
        config: rendererConfig,
        nodeIntegration: true,
        entryPoints: [
          {
            html: "./src/index.html",
            js: "./src/renderer.tsx",
            name: "main_window",
            preload: {
              js: "./src/preload.ts",
            },
          },
        ],
      },
    }),
  ],
};

export default config
