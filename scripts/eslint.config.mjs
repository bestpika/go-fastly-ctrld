import js from "../frontend/node_modules/@eslint/js/src/index.js";

export default [
  js.configs.recommended,
  {
    languageOptions: { globals: { process: "readonly", console: "readonly" } },
  },
];
