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
      'no-restricted-imports': ['error', { patterns: [{ group: ['@radix-ui/*', 'recharts'], message: 'Production UI uses PatternFly 6 only.' }] }],
    },
  },
  {
    files: ['src/presentation/**/*.{ts,tsx}'],
    rules: {
      'no-restricted-imports': ['error', { patterns: [
        { group: ['@radix-ui/*', 'recharts'], message: 'Production UI uses PatternFly 6 only.' },
        { group: ['@/infrastructure/*'], message: 'Presentation must consume application ports through the composition root.' },
      ] }],
    },
  },
])
