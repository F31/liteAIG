import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
const root = new URL("../src/", import.meta.url).pathname;
const files = [];
const walk = (dir) => {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) walk(path);
    else if (path.endsWith(".tsx") && !path.endsWith(".test.tsx"))
      files.push(path);
  }
};
walk(root);
const violations = [];
for (const file of files) {
  const source = readFileSync(file, "utf8");
  for (const match of source.matchAll(
    /(?<![=])>\s*([A-Za-z\u4e00-\u9fff][A-Za-z\u4e00-\u9fff ,.?!'-]{1,})\s*</g,
  ))
    violations.push(`${file}:${match[1].trim()}`);
  for (const match of source.matchAll(
    /(?:placeholder|title|aria-label)="([^"]*[A-Za-z\u4e00-\u9fff][^"]*)"/g,
  ))
    violations.push(`${file}:${match[1]}`);
}
if (violations.length)
  throw new Error(`Hard-coded user-visible strings:\n${violations.join("\n")}`);
