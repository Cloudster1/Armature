import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import ts from "typescript";

// An installation is one team's, so what people read says everyone or all
// projects. Only the Installation page may still name the organization it was.
const REFUSED = /organi[sz]ation/i;
const ALLOWED = ["routes/installation.tsx"];

// Where even a single lowercase word is read rather than matched on.
const READ_NAMES = new Set(["title", "label", "description", "detail", "meta", "placeholder", "aria-label", "alt", "sentence", "body", "caption"]);

function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) walk(path, out);
    else if (/\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name) && !name.endsWith(".d.ts")) out.push(path);
  }
  return out;
}

function nameOf(node: ts.Node): string | undefined {
  if (ts.isJsxAttribute(node)) return node.name.getText();
  if (ts.isPropertyAssignment(node) && (ts.isIdentifier(node.name) || ts.isStringLiteral(node.name))) return node.name.text;
  return undefined;
}

/** Whether a literal is something a person reads: a sentence, a capitalised word, or a named text slot. */
function isRead(node: ts.StringLiteral | ts.NoSubstitutionTemplateLiteral | ts.TemplateLiteralLikeNode): boolean {
  let holder: ts.Node = node.parent;
  if (ts.isTemplateSpan(holder)) holder = holder.parent;
  if (ts.isTemplateExpression(holder)) holder = holder.parent;
  if (ts.isJsxExpression(holder)) holder = holder.parent;
  if (ts.isLiteralTypeNode(holder) || ts.isImportDeclaration(holder) || ts.isExportDeclaration(holder)) return false;
  if (ts.isPropertyAssignment(holder) && holder.name === node) return false;
  if (ts.isElementAccessExpression(holder) && holder.argumentExpression === node) return false;
  const named = nameOf(holder);
  if (named?.startsWith("data-")) return false;
  if (named && READ_NAMES.has(named)) return true;
  return /\s/.test(node.text) || /^[A-Z]/.test(node.text);
}

/** The words a person would read that use the refused one, each with its line. */
function offences(source: string, fileName = "words.tsx"): string[] {
  const file = ts.createSourceFile(fileName, source, ts.ScriptTarget.Latest, true, fileName.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS);
  const found: string[] = [];
  const visit = (node: ts.Node) => {
    let text: string | undefined;
    if (ts.isJsxText(node)) text = node.text;
    else if (
      (ts.isStringLiteral(node) ||
        ts.isNoSubstitutionTemplateLiteral(node) ||
        ts.isTemplateHead(node) ||
        ts.isTemplateMiddle(node) ||
        ts.isTemplateTail(node)) &&
      isRead(node)
    )
      text = node.text;
    if (text !== undefined && REFUSED.test(text)) {
      const line = file.getLineAndCharacterOfPosition(node.getStart(file)).line + 1;
      found.push(`${line}: ${text.trim()}`);
    }
    ts.forEachChild(node, visit);
  };
  visit(file);
  return found;
}

const root = join(__dirname, "..");
// Parsing the sources it must read takes seconds on a busy machine.
const SCAN_TIMEOUT_MS = 60_000;

describe("the words on screen", () => {
  it(
    "say everyone or all projects, never the organization, outside the Installation page",
    () => {
      const found = walk(root)
        .filter((file) => !ALLOWED.includes(relative(root, file)))
        .map((file) => ({ file, text: readFileSync(file, "utf8") }))
        // Only a file that holds the word somewhere is worth parsing.
        .filter(({ text }) => REFUSED.test(text))
        .flatMap(({ file, text }) => offences(text, file).map((o) => `${relative(root, file)}:${o}`));
      expect(found.join("\n")).toBe("");
    },
    SCAN_TIMEOUT_MS,
  );

  it("are told apart from names, keys and comments", () => {
    for (const line of [
      `<p>Follow the organization</p>`,
      `<Tag>{owned ? "This project" : "Organization"}</Tag>`,
      `<Switch label="Shared with the organization" />`,
      `<Group title="Organization" />`,
      `const a = { label: "organization" };`,
      `toast.success(\`\${name} is now the organization's\`);`,
      `const words = { organization: "The organization's" };`,
      `confirm({ body: \`\${name} goes to the organization\` });`,
    ]) {
      expect(offences(line), line).not.toEqual([]);
    }
    for (const line of [
      `// The organization's own rules.`,
      `/** Absent on a field the whole organization shares. */ const x = 1;`,
      `type Source = "chosen" | "organization";`,
      `const key = ["organization", "second-factor"] as const;`,
      `<Button data-action="follow-the-organization">Follow the default</Button>`,
      `import { organizationSettingsRoute } from "./installation";`,
      `const organization = rows.filter((row) => row.organization);`,
      `const words = { organization: "Everyone's" };`,
      `if (source === "organization") go();`,
    ]) {
      expect(offences(line), line).toEqual([]);
    }
  });
});
