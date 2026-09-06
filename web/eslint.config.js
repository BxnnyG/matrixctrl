import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      globals: globals.browser,
    },
    rules: {
      // Off, deliberately.
      //
      // This rule is about dev-server ergonomics — fast refresh cannot hot-reload a
      // file that exports a helper next to its component — and it fires on almost
      // every route file here. It accounted for 59 of the 78 errors `eslint .`
      // reported, and buried the one that mattered: a hook after an early return that
      // crashed the config page for any operator who opened it with a cold cache.
      //
      // That is why nobody ever wired lint into a gate. A check whose output is 75%
      // noise is a check that gets skipped, and then the 25% is lost too.
      'react-refresh/only-export-components': 'off',

      // The crash class, and the reason this file now exists. Blocking.
      'react-hooks/rules-of-hooks': 'error',

      // Real and worth fixing, but not yet fixed. Warnings, so the gate above can be
      // switched on today rather than after a cleanup nobody has scheduled — the
      // alternative is what happened last time: a correct check, turned off.
      'react-hooks/exhaustive-deps': 'warn',
      'react-hooks/refs': 'warn',
      'react-hooks/set-state-in-effect': 'warn',
      'react-hooks/purity': 'warn',
    },
  },
])
