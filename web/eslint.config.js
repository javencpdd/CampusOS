import tsParser from '@typescript-eslint/parser'
import vueParser from 'vue-eslint-parser'

export default [
  { ignores: ['dist/**', 'node_modules/**'] },
  {
    files: ['src/**/*.{js,jsx,ts,tsx}'],
    languageOptions: { parser: tsParser, parserOptions: { sourceType: 'module', ecmaVersion: 'latest' } },
  },
  {
    files: ['src/**/*.vue'],
    languageOptions: {
      parser: vueParser,
      parserOptions: { parser: tsParser, sourceType: 'module', ecmaVersion: 'latest', extraFileExtensions: ['.vue'] },
    },
  },
]
