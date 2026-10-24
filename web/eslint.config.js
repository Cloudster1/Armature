import js from "@eslint/js";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist", "coverage", "src/api/schema.d.ts"] },
  {
    files: ["**/*.{ts,tsx}"],
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    languageOptions: {
      ecmaVersion: 2022,
      globals: globals.browser,
    },
    plugins: { "react-hooks": reactHooks },
    rules: {
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "error",
      "@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_", varsIgnorePattern: "^_" }],
      "@typescript-eslint/consistent-type-imports": "error",
      "no-restricted-syntax": [
        "error",
        {
          selector: "JSXOpeningElement[name.name='button']:not(:has(> JSXAttribute[name.name='type']))",
          message: 'Give the <button> a type: without one it submits the form it sits in. Use type="button", or type="submit" for a form\'s submit button.',
        },
      ],
    },
  },
  {
    // vi.importActual and vi.mock name a module's type by import(); Stator's and Exciter's tests never do.
    files: ["**/*.test.{ts,tsx}"],
    rules: { "@typescript-eslint/consistent-type-imports": ["error", { disallowTypeAnnotations: false }] },
  },
  {
    files: ["**/*.{js,mjs}", "vite.config.ts"],
    languageOptions: { globals: globals.node },
  },
);
