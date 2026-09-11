import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist', 'test-results', 'playwright-report']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [js.configs.recommended, tseslint.configs.recommended, reactHooks.configs.flat.recommended, reactRefresh.configs.vite],
    languageOptions: { ecmaVersion: 2020, globals: globals.browser },
    rules: {
      'no-restricted-imports': ['error', { patterns: [{ group: ['@patternfly/*'], message: 'Production UI uses Chakra UI v3; do not reintroduce PatternFly.' }] }],
    },
  },
  {
    files: ['src/presentation/**/*.{ts,tsx}'],
    rules: {
      'no-restricted-imports': ['error', { patterns: [
        { group: ['@patternfly/*'], message: 'Production UI uses Chakra UI v3; do not reintroduce PatternFly.' },
        { group: ['@/infrastructure/*'], message: 'Presentation must consume application ports through the composition root.' },
      ] }],
    },
  },
  {
    // The `components/ui/*` files are Chakra composition snippets: they deliberately export a
    // component plus its companion hooks/store (e.g. color mode, the toaster instance), which
    // is not a Fast Refresh boundary concern for these library-like modules.
    files: ['src/presentation/components/ui/**/*.{ts,tsx}'],
    rules: {
      'react-refresh/only-export-components': 'off',
    },
  },
])
