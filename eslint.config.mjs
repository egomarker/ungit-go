import js from '@eslint/js';
import globals from 'globals';
import nodePlugin from 'eslint-plugin-n';
import prettierPlugin from 'eslint-plugin-prettier/recommended';

export default [
  {
    ignores: ['public/js/**', '**/*.bundle.js'],
  },
  js.configs.recommended,
  nodePlugin.configs['flat/recommended'],
  prettierPlugin,
  {
    files: ['components/**/*.js'],
    languageOptions: {
      globals: {
        ...globals.browser,
        ungit: 'readonly',
      },
    },
    rules: {
      'n/no-missing-require': 'off',
      'n/no-unsupported-features/node-builtins': 'off',
    },
  },
  {
    files: ['public/source/**/*.js'],
    languageOptions: {
      globals: {
        ...globals.browser,
        io: 'readonly',
        jQuery: 'writable',
        ungit: 'readonly',
      },
    },
    rules: {
      'n/no-missing-require': 'off',
      'n/no-unsupported-features/node-builtins': 'off',
    },
  },
  {
    files: ['scripts/**/*.js'],
    languageOptions: {
      globals: { ...globals.node },
    },
  },
  {
    files: ['eslint.config.mjs'],
    languageOptions: { sourceType: 'module' },
  },
];
